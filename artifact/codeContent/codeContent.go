package codeContent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
	"worker_GoVer/artifact/codeGraph/strategy"
	"worker_GoVer/logger"
)

const (
	maxFileSizeBytes  = 2 * 1024 * 1024   // 파일당 최대 2MB
	maxCacheSizeBytes = 100 * 1024 * 1024 // 캐시 총합 최대 100MB
)

// GenerateCodeContent는 코드 그래프의 각 노드에 대해 소스코드를 추출하여 저장합니다.
// 반환값: 저장된 JSON 파일 경로
func GenerateCodeContent(ctx context.Context, projectPath string, graph *strategy.CodeGraph) (string, error) {
	logger.Info(ctx, "codeContent generation start", slog.Int("nodes", len(graph.Nodes)))

	// 파일별 라인 캐시 (같은 파일을 여러 번 읽지 않도록)
	fileCache := map[string][]string{}
	var cacheSizeBytes int64

	var contents []CodeContent

	for _, node := range graph.Nodes {
		lines, err := getFileLines(fileCache, &cacheSizeBytes, projectPath, node.FilePath)
		if err != nil {
			logger.Warn(ctx, "codeContent skip node",
				slog.String("filePath", node.FilePath),
				slog.String("reason", err.Error()),
			)
			continue
		}

		startIdx := node.Line - 1
		endIdx := node.EndLine
		if startIdx < 0 {
			startIdx = 0
		}
		if endIdx > len(lines) {
			endIdx = len(lines)
		}
		if startIdx >= endIdx {
			continue
		}

		content := strings.Join(lines[startIdx:endIdx], "\n")

		contents = append(contents, CodeContent{
			Type:     node.Kind,
			Name:     node.Name,
			FileName: node.FilePath,
			Package:  node.Package,
			Receiver: node.Receiver,
			Content:  content,
		})
	}

	// JSON 직렬화
	data, err := json.MarshalIndent(contents, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal code content: %w", err)
	}

	// artifact 디렉토리에 저장
	seoul, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		return "", fmt.Errorf("failed to load Seoul timezone: %w", err)
	}
	artifactDir := filepath.Join(projectPath, "artifact")
	if err := os.MkdirAll(artifactDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create artifact dir: %w", err)
	}

	fileName := fmt.Sprintf("codeContent_%s.json", time.Now().In(seoul).Format("2006-01-02-15-04-05"))
	savePath := filepath.Join(artifactDir, fileName)

	if err := os.WriteFile(savePath, data, 0644); err != nil {
		return "", fmt.Errorf("failed to write code content: %w", err)
	}

	logger.Info(ctx, "codeContent saved",
		slog.Int("entries", len(contents)),
		slog.String("path", savePath),
	)
	return savePath, nil
}

func getFileLines(cache map[string][]string, cacheSize *int64, projectPath, relPath string) ([]string, error) {
	if lines, ok := cache[relPath]; ok {
		return lines, nil
	}

	absPath := filepath.Join(projectPath, relPath)

	info, err := os.Stat(absPath)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxFileSizeBytes {
		return nil, fmt.Errorf("file too large (%d bytes), skipping cache", info.Size())
	}
	if *cacheSize+info.Size() > maxCacheSizeBytes {
		// 캐시 한도 초과 시 캐싱 없이 직접 읽기
		return readFileLines(absPath)
	}

	f, err := os.Open(absPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	cache[relPath] = lines
	*cacheSize += info.Size()
	return lines, nil
}

func readFileLines(absPath string) ([]string, error) {
	f, err := os.Open(absPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}
