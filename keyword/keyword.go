package keyword

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"worker_GoVer/ai"
	"worker_GoVer/db"
	"worker_GoVer/logger"
)

var log = logger.WithComponent("keyword")

func extractJSONObject(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "{"); i != -1 {
		if j := strings.LastIndex(s, "}"); j > i {
			return s[i : j+1]
		}
	}
	return s
}

// buildTrimmedFile은 projectContext.json에서 키워드 추출에 필요한 필드만 추출해
// 임시 파일로 저장하고 경로를 반환합니다. 호출자가 파일을 삭제해야 합니다.
func buildTrimmedFile(projCtxPath string) (string, error) {
	data, err := os.ReadFile(projCtxPath)
	if err != nil {
		return "", fmt.Errorf("failed to read projectContext: %w", err)
	}

	var raw rawProjectContext
	if err := json.Unmarshal(data, &raw); err != nil {
		return "", fmt.Errorf("failed to parse projectContext: %w", err)
	}

	modules := make([]keywordModule, 0, len(raw.ModuleDetails))
	for _, m := range raw.ModuleDetails {
		modules = append(modules, keywordModule{
			Name:         m.Module,
			Language:     m.Language,
			ExternalDeps: m.ExternalDeps,
		})
	}

	input := keywordInput{
		Project: keywordProject{
			Title:       raw.Project.Title,
			Description: raw.Project.Description,
			Goal:        raw.Project.Goal,
		},
		Overview:     raw.Analysis.Overview,
		Architecture: raw.Analysis.Architecture.Summary,
		Modules:      modules,
	}

	trimmed, err := json.Marshal(input)
	if err != nil {
		return "", fmt.Errorf("failed to marshal trimmed context: %w", err)
	}

	f, err := os.CreateTemp("", "keyword_ctx_*.json")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(trimmed); err != nil {
		os.Remove(f.Name())
		return "", fmt.Errorf("failed to write temp file: %w", err)
	}

	return f.Name(), nil
}

// ExtractAndSave는 projectContext에서 필요한 필드만 추출한 경량 파일을 AI에 전달해
// 플랫폼별 검색 키워드를 추출하고 DB에 저장합니다.
func ExtractAndSave(ctx context.Context, jobID, projectID int64, projCtxPath string) error {
	trimmedPath, err := buildTrimmedFile(projCtxPath)
	if err != nil {
		return fmt.Errorf("failed to build trimmed context: %w", err)
	}
	defer os.Remove(trimmedPath)

	p := ai.KeywordPrompt()
	log.Trace(ctx, "keyword extraction start")

	result := <-ai.GenerateMessageWithFiles(ctx, p.User, p.System, []string{trimmedPath})
	if result.Err != nil {
		return fmt.Errorf("AI keyword extraction failed: %w", result.Err)
	}

	responseStr, ok := result.Data.(string)
	if !ok {
		return fmt.Errorf("unexpected AI response type")
	}
	responseStr = extractJSONObject(responseStr)

	var resp extractResponse
	if err := json.Unmarshal([]byte(responseStr), &resp); err != nil {
		return fmt.Errorf("failed to parse keyword response: %w", err)
	}

	inputs := buildInsertInputs(jobID, projectID, resp)
	if err := db.InsertKeywords(inputs); err != nil {
		return fmt.Errorf("failed to save keywords: %w", err)
	}

	log.Trace(ctx, "keyword extraction completed",
		slog.Int("kocwCount", len(resp.KOCW)),
		slog.Int("kmoocCount", len(resp.KMOOC)),
		slog.Int("youtubeCount", len(resp.YouTube)),
	)
	return nil
}

func buildInsertInputs(jobID, projectID int64, resp extractResponse) []db.KeywordInsertInput {
	platforms := []struct {
		name     string
		keywords []string
	}{
		{"KOCW", resp.KOCW},
		{"KMOOC", resp.KMOOC},
		{"YOUTUBE", resp.YouTube},
	}

	var inputs []db.KeywordInsertInput
	for _, p := range platforms {
		for i, kw := range p.keywords {
			inputs = append(inputs, db.KeywordInsertInput{
				ProjectID:    projectID,
				JobID:        jobID,
				Platform:     p.name,
				Keyword:      kw,
				DisplayOrder: i,
			})
		}
	}
	return inputs
}
