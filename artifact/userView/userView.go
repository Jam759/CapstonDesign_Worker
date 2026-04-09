package userView

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
	"worker_GoVer/ai"
	"worker_GoVer/db"
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
func Generate(input GenerateInput, projCtxPath string, projectPath string) (string, error) {
	log.Printf("[UserView] start v%d userID=%d", input.Version, input.UserID)

	p := ai.UserViewPrompt()
	result := <-ai.GenerateMessageWithFiles(p.User, p.System, []string{projCtxPath})
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

	log.Printf("[UserView] saved: %s", savePath)
	return savePath, nil
}

// Persist는 userView 파일을 S3에 업로드하고 project_analysis_reports(USER_VIEW)에 저장합니다.
// 반환값: 삽입된 project_meta_reports_id (실패 시 0)
func Persist(filePath string, jobID int64, installationID int64, repoID int64, projectID int64, version int, s3Bucket string, beforeCommit string, afterCommit string) int64 {
	url, err := s3.UploadUserView(installationID, repoID, filePath)
	if err != nil {
		log.Printf("[UserView] failed to upload to S3: %v", err)
		return 0
	}

	var sizeBytes int64
	if info, err := os.Stat(filePath); err == nil {
		sizeBytes = info.Size()
	}

	id, err := db.InsertAnalysisReport(projectID, jobID, "USER_VIEW", version, s3Bucket, url, sizeBytes, beforeCommit, afterCommit)
	if err != nil {
		log.Printf("[UserView] failed to insert report: %v", err)
		return 0
	}
	log.Printf("[UserView] USER_VIEW v%d saved: %s", version, url)
	return id
}
