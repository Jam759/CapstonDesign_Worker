package codeGraph

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
	strategy "worker_GoVer/artifact/codeGraph/strategy"
	"worker_GoVer/logger"
)

var log = logger.WithComponent("codeGraph")

// 등록된 전략 목록
var strategies = []strategy.CodeGraphStrategy{
	strategy.GoStrategy{},
	strategy.PythonStrategy{},
	strategy.JavaStrategy{},
}

// GenerateCodeGraph는 프로젝트의 코드 그래프를 생성하고 artifact에 저장합니다.
// 반환값: 저장된 JSON 파일 경로
func GenerateCodeGraph(ctx context.Context, projectPath string) (string, error) {
	log.Trace(ctx, "codeGraph analysis start", slog.String("projectPath", projectPath))

	// 1. 프로젝트에 존재하는 파일 확장자 수집
	extSet := map[string]bool{}
	filepath.Walk(projectPath, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != "" {
			extSet[ext] = true
		}
		return nil
	})

	// 2. 매칭되는 전략 실행 및 결과 병합
	merged := &strategy.CodeGraph{
		Language: "multi",
	}
	matchedCount := 0

	for _, s := range strategies {
		matched := false
		for _, ext := range s.SupportedExtensions() {
			if extSet[ext] {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}

		graph, err := s.Analyze(ctx, projectPath)
		if err != nil {
			return "", fmt.Errorf("failed to analyze with %T: %w", s, err)
		}

		matchedCount++
		if matchedCount == 1 {
			merged.Language = graph.Language
		}
		merged.Nodes = append(merged.Nodes, graph.Nodes...)
		merged.Edges = append(merged.Edges, graph.Edges...)
		merged.Imports = append(merged.Imports, graph.Imports...)
	}

	if matchedCount == 0 {
		exts := make([]string, 0, len(extSet))
		for e := range extSet {
			exts = append(exts, e)
		}
		log.Warn(ctx, "no supported language detected", nil, slog.Any("extensions", exts))
		return "", fmt.Errorf("no supported language found in project")
	}

	log.Trace(ctx, "codeGraph merged",
		slog.String("language", merged.Language),
		slog.Int("nodes", len(merged.Nodes)),
		slog.Int("edges", len(merged.Edges)),
		slog.Int("imports", len(merged.Imports)),
	)

	// 3. JSON 직렬화
	data, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal code graph: %w", err)
	}

	// 4. artifact 디렉토리에 저장
	seoul, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		return "", fmt.Errorf("failed to load Seoul timezone: %w", err)
	}
	artifactDir := filepath.Join(projectPath, "artifact")
	if err := os.MkdirAll(artifactDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create artifact dir: %w", err)
	}

	fileName := fmt.Sprintf("codeGraph_%s.json", time.Now().In(seoul).Format("2006-01-02-15-04-05"))
	savePath := filepath.Join(artifactDir, fileName)

	if err := os.WriteFile(savePath, data, 0644); err != nil {
		return "", fmt.Errorf("failed to write code graph: %w", err)
	}

	log.Trace(ctx, "codeGraph saved", slog.String("path", savePath))
	return savePath, nil
}
