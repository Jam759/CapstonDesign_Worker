package userView

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
	"worker_GoVer/ai"
	"worker_GoVer/db"
	"worker_GoVer/logger"
	"worker_GoVer/s3"
)

func extractJSONObject(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "{"); i != -1 {
		if j := strings.LastIndex(s, "}"); j > i {
			return s[i : j+1]
		}
	}
	return s
}

// Generate는 projectContext 파일 기반으로 UserView를 AI 생성하고 파일로 저장합니다.
// 반환값: 저장된 파일 경로
func Generate(ctx context.Context, input GenerateInput, projCtxPath string, projectPath string) (string, error) {
	logger.Info(ctx, "userView generation start",
		slog.Int("version", input.Version),
		slog.Int64("userId", input.UserID),
	)

	p := ai.UserViewPrompt()
	result := <-ai.GenerateMessageWithFiles(ctx, p.User, p.System, []string{projCtxPath})
	if result.Err != nil {
		return "", fmt.Errorf("AI generation failed: %w", result.Err)
	}

	responseStr, ok := result.Data.(string)
	if !ok {
		return "", fmt.Errorf("unexpected AI response type")
	}
	responseStr = extractJSONObject(responseStr)

	var aiPart struct {
		Headline  string    `json:"headline"`
		Summary   string    `json:"summary"`
		Strengths []string  `json:"strengths"`
		Risks     []string  `json:"risks"`
		Advice    []Advice  `json:"advice"`
		Scorecard Scorecard `json:"scorecard"`
	}
	if err := json.Unmarshal([]byte(responseStr), &aiPart); err != nil {
		return "", fmt.Errorf("failed to parse AI response: %w", err)
	}

	seoul, _ := time.LoadLocation("Asia/Seoul")
	now := time.Now().In(seoul)

	view := UserView{
		SchemaVersion: "1.0.0",
		GeneratedAt:   now.Format(time.RFC3339),
		Scope: Scope{
			ProjectID:          input.ProjectID,
			UserID:             input.UserID,
			RepositoryFullName: input.RepositoryFullName,
			BranchName:         input.BranchName,
			BeforeCommitHash:   input.BeforeCommitHash,
			AfterCommitHash:    input.AfterCommitHash,
		},
		Headline:  aiPart.Headline,
		Summary:   aiPart.Summary,
		Strengths: aiPart.Strengths,
		Risks:     aiPart.Risks,
		Advice:    aiPart.Advice,
		Scorecard: aiPart.Scorecard,
		QuestSummary: QuestSummary{
			CompletedQuestIDs: input.CompletedQuestIDs,
			NewQuestIDs:       input.NewQuestIDs,
		},
	}

	data, err := json.MarshalIndent(view, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal user view: %w", err)
	}

	artifactDir := filepath.Join(projectPath, "artifact")
	if err := os.MkdirAll(artifactDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create artifact dir: %w", err)
	}

	fileName := fmt.Sprintf("userView_v%d_%s.json", input.Version, now.Format("2006-01-02-15-04-05"))
	savePath := filepath.Join(artifactDir, fileName)

	if err := os.WriteFile(savePath, data, 0644); err != nil {
		return "", fmt.Errorf("failed to write user view: %w", err)
	}

	logger.Info(ctx, "userView saved", slog.String("path", savePath))
	return savePath, nil
}

// Persist는 userView 파일을 S3에 업로드하고 project_analysis_reports(USER_VIEW)에 저장합니다.
// 반환값: (삽입된 project_analysis_reports_id, S3 URL, error)
// S3 업로드 성공 후 DB 삽입이 실패한 경우에도 url은 반환하므로 호출자가 S3 오브젝트를 정리할 수 있습니다.
func Persist(ctx context.Context, filePath string, newKBID *int64, installationID int64, repoID int64, projectID int64, version int, s3Bucket string, beforeCommit string, afterCommit string) (int64, string, error) {
	url, err := s3.UploadUserView(ctx, installationID, repoID, filePath)
	if err != nil {
		return 0, "", fmt.Errorf("userView S3 upload failed: %w", err)
	}

	var sizeBytes int64
	if info, err := os.Stat(filePath); err == nil {
		sizeBytes = info.Size()
	}

	id, err := db.InsertAnalysisReport(projectID, newKBID, "USER_VIEW", version, s3Bucket, url, sizeBytes, beforeCommit, afterCommit)
	if err != nil {
		return 0, url, fmt.Errorf("userView DB insert failed: %w", err)
	}
	logger.Info(ctx, "USER_VIEW saved",
		slog.Int("version", version),
		slog.String("url", url),
	)
	return id, url, nil
}
