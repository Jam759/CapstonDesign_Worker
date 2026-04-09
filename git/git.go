package git

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"
	"worker_GoVer/config"
	"worker_GoVer/logger"

	"github.com/google/uuid"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/golang-jwt/jwt/v5"
)

// GitHub App JWT 생성 (10분 유효)
func generateAppJWT(appID string, privateKey *rsa.PrivateKey) (string, error) {
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Issuer:    appID,
		IssuedAt:  jwt.NewNumericDate(now.Add(-60 * time.Second)),
		ExpiresAt: jwt.NewNumericDate(now.Add(10 * time.Minute)),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	return token.SignedString(privateKey)
}

// Installation Access Token 발급
func getInstallationToken(appJWT string, installationID int64) (string, error) {
	url := fmt.Sprintf("https://api.github.com/app/installations/%d/access_tokens", installationID)

	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+appJWT)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to request installation token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("failed to get installation token: status %d body=%s", resp.StatusCode, string(body))
	}

	var result struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	return result.Token, nil
}

// CloneRepository는 GitHub App installation을 통해 repository를 clone합니다.
// localPath: clone할 로컬 경로 (호출자가 결정)
// repoFullName: "owner/repo" 형식
// branchName: 클론할 브랜치명
func CloneRepository(ctx context.Context, installationID int64, repoFullName string, localPath string, branchName string) error {
	logger.Info(ctx, "git clone start",
		slog.String("repo", repoFullName),
		slog.String("branch", branchName),
		slog.Int64("installationID", installationID),
		slog.String("localPath", localPath),
	)
	cfg := config.Get()

	// 1. Private key 파싱
	privateKey, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(cfg.GitHubAppPrivateKey))
	if err != nil {
		return fmt.Errorf("failed to parse private key: %w", err)
	}

	// 2. App JWT 생성
	appJWT, err := generateAppJWT(cfg.GitHubAppID, privateKey)
	if err != nil {
		return fmt.Errorf("failed to generate app JWT: %w", err)
	}

	// 3. Installation Access Token 발급
	installationToken, err := getInstallationToken(appJWT, installationID)
	if err != nil {
		return err
	}

	// 4. Clone 실행
	cloneURL := fmt.Sprintf("https://github.com/%s.git", repoFullName)
	_, err = git.PlainClone(localPath, false, &git.CloneOptions{
		URL:           cloneURL,
		ReferenceName: plumbing.NewBranchReferenceName(branchName),
		Auth: &githttp.BasicAuth{
			Username: "x-access-token",
			Password: installationToken,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to clone repository: %w", err)
	}

	logger.Info(ctx, "git clone done", slog.String("repo", repoFullName))
	return nil
}

// CheckoutBranch는 원격 브랜치(origin/{branchName})의 최신 커밋으로 working tree를 이동합니다.
// clone/fetch 후 항상 호출해야 올바른 브랜치 상태가 보장됩니다.
func CheckoutBranch(ctx context.Context, clonePath string, branchName string) error {
	_ = ctx
	repo, err := git.PlainOpen(clonePath)
	if err != nil {
		return fmt.Errorf("failed to open repository: %w", err)
	}

	remoteRef, err := repo.Reference(plumbing.NewRemoteReferenceName("origin", branchName), true)
	if err != nil {
		return fmt.Errorf("failed to resolve remote branch origin/%s: %w", branchName, err)
	}

	w, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("failed to get worktree: %w", err)
	}

	if err := w.Checkout(&git.CheckoutOptions{
		Hash:  remoteRef.Hash(),
		Force: true,
	}); err != nil {
		return fmt.Errorf("failed to checkout branch %s: %w", branchName, err)
	}

	return nil
}

// Checkout은 로컬 저장소를 특정 commit으로 이동합니다.
func Checkout(ctx context.Context, clonePath string, commitSHA string) error {
	_ = ctx
	repo, err := git.PlainOpen(clonePath)
	if err != nil {
		return fmt.Errorf("failed to open repository: %w", err)
	}

	w, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("failed to get worktree: %w", err)
	}

	if err := w.Checkout(&git.CheckoutOptions{
		Hash:  plumbing.NewHash(commitSHA),
		Force: true,
	}); err != nil {
		return fmt.Errorf("failed to checkout %s: %w", commitSHA, err)
	}

	return nil
}

// DiffFileList는 두 commit 간 변경된 파일 목록을 반환합니다.
func DiffFileList(ctx context.Context, clonePath, beforeCommitSHA, afterCommitSHA string, isMerge bool) ([]FileDiffStat, error) {
	_ = ctx
	repo, err := git.PlainOpen(clonePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open repository: %w", err)
	}

	afterHash := plumbing.NewHash(afterCommitSHA)
	afterCommit, err := repo.CommitObject(afterHash)
	if err != nil {
		return nil, fmt.Errorf("failed to get after commit: %w", err)
	}

	var beforeCommit *object.Commit
	if isMerge {
		if afterCommit.NumParents() == 0 {
			return nil, fmt.Errorf("merge commit has no parents")
		}
		beforeCommit, err = afterCommit.Parent(0)
		if err != nil {
			return nil, fmt.Errorf("failed to get parent commit: %w", err)
		}
	} else {
		beforeHash := plumbing.NewHash(beforeCommitSHA)
		beforeCommit, err = repo.CommitObject(beforeHash)
		if err != nil {
			return nil, fmt.Errorf("failed to get before commit: %w", err)
		}
	}

	beforeTree, err := beforeCommit.Tree()
	if err != nil {
		return nil, fmt.Errorf("failed to get before tree: %w", err)
	}
	afterTree, err := afterCommit.Tree()
	if err != nil {
		return nil, fmt.Errorf("failed to get after tree: %w", err)
	}

	changes, err := beforeTree.Diff(afterTree)
	if err != nil {
		return nil, fmt.Errorf("failed to compute diff: %w", err)
	}

	stats := make([]FileDiffStat, 0, len(changes))
	for _, change := range changes {
		from := change.From.Name
		to := change.To.Name

		var stat FileDiffStat
		switch {
		case from == "" && to != "":
			stat = FileDiffStat{Path: to, Status: "added"}
		case from != "" && to == "":
			stat = FileDiffStat{Path: from, Status: "deleted"}
		case from != to:
			stat = FileDiffStat{Path: to, PreviousPath: from, Status: "renamed"}
		default:
			stat = FileDiffStat{Path: to, Status: "modified"}
		}
		stats = append(stats, stat)
	}
	return stats, nil
}

// Diff는 두 commit 간의 unified diff를 반환합니다.
// isMerge=true: afterCommitSHA의 첫 번째 부모와 afterCommitSHA 간 diff
// isMerge=false: beforeCommitSHA와 afterCommitSHA 간 diff
func Diff(ctx context.Context, clonePath string, beforeCommitSHA string, afterCommitSHA string, isMerge bool) (string, error) {
	_ = ctx
	repo, err := git.PlainOpen(clonePath)
	if err != nil {
		return "", fmt.Errorf("failed to open repository: %w", err)
	}

	// after commit 가져오기
	afterHash := plumbing.NewHash(afterCommitSHA)
	afterCommit, err := repo.CommitObject(afterHash)
	if err != nil {
		return "", fmt.Errorf("failed to get after commit: %w", err)
	}

	var beforeCommit *object.Commit
	if isMerge {
		// merge commit의 첫 번째 부모 기준
		if afterCommit.NumParents() == 0 {
			return "", fmt.Errorf("merge commit has no parents")
		}
		beforeCommit, err = afterCommit.Parent(0)
		if err != nil {
			return "", fmt.Errorf("failed to get parent commit: %w", err)
		}
	} else {
		beforeHash := plumbing.NewHash(beforeCommitSHA)
		beforeCommit, err = repo.CommitObject(beforeHash)
		if err != nil {
			return "", fmt.Errorf("failed to get before commit: %w", err)
		}
	}

	// 각 commit의 tree 가져오기
	beforeTree, err := beforeCommit.Tree()
	if err != nil {
		return "", fmt.Errorf("failed to get before tree: %w", err)
	}

	afterTree, err := afterCommit.Tree()
	if err != nil {
		return "", fmt.Errorf("failed to get after tree: %w", err)
	}

	// diff 생성
	changes, err := beforeTree.Diff(afterTree)
	if err != nil {
		return "", fmt.Errorf("failed to compute diff: %w", err)
	}

	patch, err := changes.Patch()
	if err != nil {
		return "", fmt.Errorf("failed to generate patch: %w", err)
	}

	// artifact 디렉토리에 diff 파일 저장
	seoul, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		return "", fmt.Errorf("failed to load Seoul timezone: %w", err)
	}
	artifactDir := filepath.Join(clonePath, "artifact")
	if err := os.MkdirAll(artifactDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create artifact dir: %w", err)
	}

	fileName := fmt.Sprintf("%s%s.diff", uuid.New().String(), time.Now().In(seoul).Format("2006-01-02-15-04-05"))
	diffPath := filepath.Join(artifactDir, fileName)

	if err := os.WriteFile(diffPath, []byte(patch.String()), 0644); err != nil {
		return "", fmt.Errorf("failed to write diff file: %w", err)
	}

	return diffPath, nil
}

// Fetch는 원격 저장소의 최신 상태를 가져옵니다.
func Fetch(ctx context.Context, clonePath string, installationID int64) error {
	logger.Info(ctx, "git fetch start",
		slog.Int64("installationID", installationID),
		slog.String("clonePath", clonePath),
	)
	cfg := config.Get()

	privateKey, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(cfg.GitHubAppPrivateKey))
	if err != nil {
		return fmt.Errorf("failed to parse private key: %w", err)
	}

	appJWT, err := generateAppJWT(cfg.GitHubAppID, privateKey)
	if err != nil {
		return fmt.Errorf("failed to generate app JWT: %w", err)
	}

	installationToken, err := getInstallationToken(appJWT, installationID)
	if err != nil {
		return err
	}

	repo, err := git.PlainOpen(clonePath)
	if err != nil {
		return fmt.Errorf("failed to open repository: %w", err)
	}

	err = repo.Fetch(&git.FetchOptions{
		Auth: &githttp.BasicAuth{
			Username: "x-access-token",
			Password: installationToken,
		},
	})
	if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return fmt.Errorf("failed to fetch: %w", err)
	}

	logger.Info(ctx, "git fetch done", slog.Int64("installationID", installationID))
	return nil
}
