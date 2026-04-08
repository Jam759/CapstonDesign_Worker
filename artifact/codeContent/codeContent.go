package codeContent

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
	"worker_GoVer/artifact/codeGraph/strategy"
)

// GenerateCodeContent는 코드 그래프의 각 노드에 대해 소스코드를 추출하여 저장합니다.
// 반환값: 저장된 JSON 파일 경로
func GenerateCodeContent(projectPath string, graph *strategy.CodeGraph) (string, error) {
	log.Printf("[CodeContent] start: nodes=%d", len(graph.Nodes))

	// 파일별 라인 캐시 (같은 파일을 여러 번 읽지 않도록)
	fileCache := map[string][]string{}

	var contents []CodeContent

	for _, node := range graph.Nodes {
		lines, err := getFileLines(fileCache, projectPath, node.FilePath)
		if err != nil {
			continue // 파일 읽기 실패 시 스킵
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

	log.Printf("[CodeContent] saved: entries=%d path=%s", len(contents), savePath)
	return savePath, nil
}

func getFileLines(cache map[string][]string, projectPath, relPath string) ([]string, error) {
	if lines, ok := cache[relPath]; ok {
		return lines, nil
	}

	absPath := filepath.Join(projectPath, relPath)
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
	return lines, nil
}
