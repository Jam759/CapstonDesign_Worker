package strategy

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"worker_GoVer/artifact/codeContent"
	"worker_GoVer/artifact/codeGraph"
	codeGraphStrategy "worker_GoVer/artifact/codeGraph/strategy"
	"worker_GoVer/artifact/projectContext"
	"worker_GoVer/artifact/userView"
	"worker_GoVer/config"
	"worker_GoVer/db"
	"worker_GoVer/disk"
	"worker_GoVer/git"
)

type FullScanStrategy struct{}

func (s FullScanStrategy) Handle(_ context.Context, jobID string, data json.RawMessage) error {
	var msg FullScanQueueMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return fmt.Errorf("failed to unmarshal message: %w", err)
	}

	log.Printf("[FullScan] start jobId=%s repo=%s branch=%s installationId=%d repoId=%d projectId=%d",
		jobID, msg.RepositoryFullName, msg.BranchName,
		msg.InstallationID, msg.RepositoryID, msg.ProjectID,
	)

	cfg := config.Get()
	jobIDInt := msg.JobID
	if jobIDInt == 0 {
		jobIDInt, _ = strconv.ParseInt(jobID, 10, 64)
	}

	// 1. 디스크 정리
	if err := disk.IfNeedDoCleanWorkspace(); err != nil {
		log.Printf("[FullScan] workspace cleanup warning: %v", err)
	}

	// 2. 로컬 경로 결정: WorkspaceBaseDir/{installationId}/{repoId}
	localPath := filepath.Join(cfg.WorkspaceBaseDir,
		strconv.FormatInt(msg.InstallationID, 10),
		strconv.FormatInt(msg.RepositoryID, 10),
	)

	// 3. clone or fetch
	exists, err := disk.IsExistDir(localPath)
	if err != nil {
		return fmt.Errorf("failed to check dir: %w", err)
	}
	if exists {
		if err := git.Fetch(localPath, msg.InstallationID); err != nil {
			return fmt.Errorf("failed to fetch: %w", err)
		}
	} else {
		if err := git.CloneRepository(msg.InstallationID, msg.RepositoryFullName, localPath); err != nil {
			return fmt.Errorf("failed to clone: %w", err)
		}
	}

	// 4. lock
	if locked, _ := disk.IsLocked(localPath); locked {
		return fmt.Errorf("repository is locked, skipping jobId=%s", jobID)
	}
	if _, err := disk.CreateLockFileAtomic(localPath); err != nil {
		return fmt.Errorf("failed to lock: %w", err)
	}
	defer func() {
		if err := disk.RemoveLockAtomic(localPath); err != nil {
			log.Printf("[FullScan] failed to remove lock: %v", err)
		}
	}()

	// 5. CodeGraph 생성
	graphPath, err := codeGraph.GenerateCodeGraph(localPath)
	if err != nil {
		return fmt.Errorf("failed to generate code graph: %w", err)
	}

	// 6. CodeContent 생성
	graphData, err := os.ReadFile(graphPath)
	if err != nil {
		return fmt.Errorf("failed to read code graph: %w", err)
	}
	var graph codeGraphStrategy.CodeGraph
	if err := json.Unmarshal(graphData, &graph); err != nil {
		return fmt.Errorf("failed to parse code graph: %w", err)
	}
	contentPath, err := codeContent.GenerateCodeContent(localPath, &graph)
	if err != nil {
		return fmt.Errorf("failed to generate code content: %w", err)
	}

	// 7. ProjectContext 생성 (AI 분석, 파일로 전달)
	ctxVersion, err := db.NextReportVersion(msg.ProjectID, "PROJECT_KB")
	if err != nil {
		log.Printf("[FullScan] failed to get project context version (non-fatal): %v", err)
		ctxVersion = 1
	}
	ctxPath, err := projectContext.GenerateProjectContext(localPath, graphPath, contentPath, ctxVersion)
	if err != nil {
		return fmt.Errorf("failed to generate project context: %w", err)
	}

	// 7-1. ProjectContext S3 업로드 + DB 저장
	projectContext.Persist(ctxPath, jobIDInt, msg.InstallationID, msg.RepositoryID, msg.ProjectID, ctxVersion, cfg.AWSS3Bucket, "", "")

	// 8. Quest 평가 및 생성 (FullScan은 userID 없으므로 스킵)

	// 9. UserView 생성
	version, err := db.NextReportVersion(msg.ProjectID, "USER_VIEW")
	if err != nil {
		log.Printf("[FullScan] failed to get user view version (non-fatal): %v", err)
		version = 1
	}
	uvInput := userView.GenerateInput{
		ProjectID:          msg.ProjectID,
		RepositoryFullName: msg.RepositoryFullName,
		BranchName:         msg.BranchName,
		Version:            version,
	}
	if uvPath, err := userView.Generate(uvInput, ctxPath, localPath); err != nil {
		log.Printf("[FullScan] failed to generate user view (non-fatal): %v", err)
	} else {
		userView.Persist(uvPath, jobIDInt, msg.InstallationID, msg.RepositoryID, msg.ProjectID, version, cfg.AWSS3Bucket, "", "")
	}

	// 10. touch 파일 업데이트
	if _, err := disk.CreateTouchFileAtomic(localPath); err != nil {
		log.Printf("[FullScan] failed to touch: %v", err)
	}

	log.Printf("[FullScan] done jobId=%s repo=%s", jobID, msg.RepositoryFullName)
	return nil
}

