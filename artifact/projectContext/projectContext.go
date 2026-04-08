package projectContext

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"worker_GoVer/ai"
	"worker_GoVer/artifact/codeGraph/strategy"
	"worker_GoVer/db"
	"worker_GoVer/s3"
)

// GenerateProjectContext는 codeGraph + codeContent 기반으로 프로젝트 분석 문서를 생성합니다.
// 흐름: metrics 계산 → signals 정적 분석 → 모듈별 AI 분석(병렬) → 전체 AI 분석 + signals 보정 → 저장
func GenerateProjectContext(projectPath string, graphPath string, contentPath string, version int) (string, error) {
	log.Printf("[ProjectContext] start v%d", version)

	// 파일 읽기
	graphData, err := os.ReadFile(graphPath)
	if err != nil {
		return "", fmt.Errorf("failed to read code graph: %w", err)
	}
	var graph strategy.CodeGraph
	if err := json.Unmarshal(graphData, &graph); err != nil {
		return "", fmt.Errorf("failed to parse code graph: %w", err)
	}

	contentJSON, err := os.ReadFile(contentPath)
	if err != nil {
		return "", fmt.Errorf("failed to read code content: %w", err)
	}

	// 1. 정량 메트릭 계산 (codeGraph 기반, AI 불필요)
	metrics := CalculateMetrics(&graph)

	// 2. codeContent 파싱
	var contents []map[string]any
	if err := json.Unmarshal(contentJSON, &contents); err != nil {
		return "", fmt.Errorf("failed to parse codeContent: %w", err)
	}

	// 3. 정적 분석 신호 계산 (초기값)
	signals := CalculateSignals(&graph, contents)

	// 4. 모듈 청크 분할 후 병렬 AI 분석
	moduleNodes := groupNodesByModule(&graph)
	moduleImports := groupImportsByModule(&graph)
	moduleContents := groupContentsByModule(contents)

	moduleNames := make([]string, 0, len(moduleNodes))
	for mod := range moduleNodes {
		moduleNames = append(moduleNames, mod)
	}

	chunks := chunkModulesByNodeCount(moduleNames, moduleNodes, 5)
	log.Printf("[ProjectContext] metrics calculated, signals analyzed, starting chunk AI analysis: modules=%d chunks=%d", len(moduleNames), len(chunks))

	type chunkResult struct {
		details []ModuleDetail
		err     error
	}
	chunkResults := make([]chunkResult, len(chunks))
	var wg sync.WaitGroup

	for i, chunk := range chunks {
		wg.Add(1)
		go func(idx int, mods []string) {
			defer wg.Done()
			ds, err := analyzeModuleChunk(graph.Language, mods, moduleNodes, moduleImports, moduleContents)
			chunkResults[idx] = chunkResult{details: ds, err: err}
		}(i, chunk)
	}
	wg.Wait()

	var details []ModuleDetail
	for _, r := range chunkResults {
		if r.err != nil {
			return "", fmt.Errorf("failed to analyze module chunk: %w", r.err)
		}
		details = append(details, r.details...)
	}

	log.Printf("[ProjectContext] module analysis done: %d modules", len(details))

	// 5. 전체 프로젝트 분석 + signals AI 보정 (파일 전달)
	log.Printf("[ProjectContext] generating overview...")
	analysis, correctedSignals, err := generateOverview(details, &graph, metrics, signals, graphPath, contentPath)
	if err != nil {
		return "", fmt.Errorf("failed to generate overview: %w", err)
	}

	// AI 보정된 signals 적용
	if correctedSignals != nil {
		signals = *correctedSignals
	}

	// 6. ProjectContext 조립
	seoul, _ := time.LoadLocation("Asia/Seoul")
	ctx := ProjectContext{
		Metrics:       metrics,
		Signals:       signals,
		Analysis:      *analysis,
		ModuleDetails: details,
		CodeGraph:     &graph,
		GeneratedAt:   time.Now().In(seoul).Format(time.RFC3339),
	}

	// 7. 저장
	data, err := json.MarshalIndent(ctx, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal project context: %w", err)
	}

	artifactDir := filepath.Join(projectPath, "artifact")
	if err := os.MkdirAll(artifactDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create artifact dir: %w", err)
	}

	fileName := fmt.Sprintf("projectContext_v%d_%s.json", version, time.Now().In(seoul).Format("2006-01-02-15-04-05"))
	savePath := filepath.Join(artifactDir, fileName)

	if err := os.WriteFile(savePath, data, 0644); err != nil {
		return "", fmt.Errorf("failed to write project context: %w", err)
	}

	log.Printf("[ProjectContext] saved: %s", savePath)
	return savePath, nil
}

// chunkModulesByNodeCount는 모듈을 최대 maxChunks개 청크로 균등 분할합니다.
// 노드 수가 많은 모듈부터 정렬한 뒤 총 노드 수 기반으로 청크당 임계값을 계산합니다.
func chunkModulesByNodeCount(moduleNames []string, moduleNodes map[string][]strategy.Node, maxChunks int) [][]string {
	if len(moduleNames) == 0 {
		return nil
	}

	sorted := make([]string, len(moduleNames))
	copy(sorted, moduleNames)
	sort.Slice(sorted, func(i, j int) bool {
		return len(moduleNodes[sorted[i]]) > len(moduleNodes[sorted[j]])
	})

	// 총 노드 수 기반으로 청크당 임계값 산출
	totalNodes := 0
	for _, mod := range sorted {
		totalNodes += len(moduleNodes[mod])
	}
	nodesPerChunk := (totalNodes + maxChunks - 1) / maxChunks
	if nodesPerChunk < 1 {
		nodesPerChunk = 1
	}

	var chunks [][]string
	var cur []string
	curCount := 0

	for _, mod := range sorted {
		n := len(moduleNodes[mod])
		if len(cur) > 0 && curCount+n > nodesPerChunk && len(chunks) < maxChunks-1 {
			chunks = append(chunks, cur)
			cur = nil
			curCount = 0
		}
		cur = append(cur, mod)
		curCount += n
	}
	if len(cur) > 0 {
		chunks = append(chunks, cur)
	}
	return chunks
}

// moduleChunkInput은 청크 파일 형식입니다.
type moduleChunkInput struct {
	Language string            `json:"language"`
	Modules  []moduleChunkData `json:"modules"`
}

type moduleChunkData struct {
	Module   string           `json:"module"`
	Nodes    []strategy.Node  `json:"nodes"`
	Imports  []strategy.Import `json:"imports"`
	Contents []map[string]any `json:"contents"`
}

// analyzeModuleChunk는 청크에 포함된 모듈들을 파일 업로드 방식으로 AI 분석합니다.
func analyzeModuleChunk(language string, mods []string, moduleNodes map[string][]strategy.Node, moduleImports map[string][]strategy.Import, moduleContents map[string][]map[string]any) ([]ModuleDetail, error) {
	// 청크 데이터 구성
	chunk := moduleChunkInput{Language: language}
	for _, mod := range mods {
		chunk.Modules = append(chunk.Modules, moduleChunkData{
			Module:   mod,
			Nodes:    moduleNodes[mod],
			Imports:  moduleImports[mod],
			Contents: moduleContents[mod],
		})
	}

	// 임시 파일에 저장
	chunkPath, err := writeTempJSON("chunk_*.json", chunk)
	if err != nil {
		return nil, fmt.Errorf("failed to write chunk file: %w", err)
	}
	defer os.Remove(chunkPath)

	p := ai.ModuleChunkPrompt()
	result := <-ai.GenerateMessageWithFiles(p.User, p.System, []string{chunkPath})
	if result.Err != nil {
		return nil, result.Err
	}

	responseStr, ok := result.Data.(string)
	if !ok {
		return nil, fmt.Errorf("unexpected response type")
	}

	responseStr = extractJSONArray(responseStr)

	var details []ModuleDetail
	if err := json.Unmarshal([]byte(responseStr), &details); err != nil {
		// 파싱 실패 시 모듈별 fallback
		details = make([]ModuleDetail, len(mods))
		for i, mod := range mods {
			details[i] = ModuleDetail{Module: mod, Language: language, Summary: responseStr}
		}
		return details, nil
	}

	// 모듈명·언어 보정
	for i := range details {
		if details[i].Module == "" && i < len(mods) {
			details[i].Module = mods[i]
		}
		details[i].Language = language
	}
	return details, nil
}

// 전체 프로젝트 분석 + signals 보정 (모든 데이터 파일로 전달)
func generateOverview(details []ModuleDetail, graph *strategy.CodeGraph, metrics Metrics, signals Signals, graphPath string, contentPath string) (*Analysis, *Signals, error) {
	detailsPath, err := writeTempJSON("moduleDetails_*.json", details)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to write module details: %w", err)
	}
	defer os.Remove(detailsPath)

	metricsPath, err := writeTempJSON("metrics_*.json", metrics)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to write metrics: %w", err)
	}
	defer os.Remove(metricsPath)

	signalsPath, err := writeTempJSON("signals_*.json", signals)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to write signals: %w", err)
	}
	defer os.Remove(signalsPath)

	p := ai.ProjectOverviewPrompt()
	result := <-ai.GenerateMessageWithFiles(p.User, p.System, []string{graphPath, contentPath, detailsPath, metricsPath, signalsPath})
	if result.Err != nil {
		return nil, nil, result.Err
	}

	responseStr, ok := result.Data.(string)
	if !ok {
		return nil, nil, fmt.Errorf("unexpected response type")
	}

	responseStr = extractJSONObject(responseStr)

	var parsed struct {
		Analysis          Analysis `json:"analysis"`
		SignalCorrections *Signals `json:"signalCorrections"`
	}
	if err := json.Unmarshal([]byte(responseStr), &parsed); err != nil {
		return &Analysis{Overview: responseStr}, nil, nil
	}

	return &parsed.Analysis, parsed.SignalCorrections, nil
}

// writeTempJSON은 v를 JSON으로 직렬화하여 임시 파일에 저장하고 경로를 반환합니다.
func writeTempJSON(pattern string, v any) (string, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	f, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// extractJSONArray는 응답 문자열에서 JSON 배열([...])을 추출합니다.
func extractJSONArray(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "["); i != -1 {
		if j := strings.LastIndex(s, "]"); j > i {
			return s[i : j+1]
		}
	}
	return s
}

// extractJSONObject는 응답 문자열에서 JSON 객체({...})를 추출합니다.
func extractJSONObject(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "{"); i != -1 {
		if j := strings.LastIndex(s, "}"); j > i {
			return s[i : j+1]
		}
	}
	return s
}

// Persist는 projectContext 파일을 S3에 업로드하고 project_analysis_reports(PROJECT_KB)에 저장합니다.
func Persist(filePath string, jobID int64, installationID int64, repoID int64, projectID int64, version int, s3Bucket string, beforeCommit string, afterCommit string) {
	url, err := s3.UploadProjectContext(installationID, repoID, filePath)
	if err != nil {
		log.Printf("[ProjectContext] failed to upload to S3: %v", err)
		return
	}

	var sizeBytes int64
	if info, err := os.Stat(filePath); err == nil {
		sizeBytes = info.Size()
	}

	if err := db.InsertAnalysisReport(projectID, jobID, "PROJECT_KB", version, s3Bucket, url, sizeBytes, beforeCommit, afterCommit); err != nil {
		log.Printf("[ProjectContext] failed to insert report: %v", err)
	} else {
		log.Printf("[ProjectContext] PROJECT_KB v%d saved: %s", version, url)
	}
}

func groupNodesByModule(graph *strategy.CodeGraph) map[string][]strategy.Node {
	m := map[string][]strategy.Node{}
	for _, node := range graph.Nodes {
		key := node.Package
		if key == "" {
			key = filepath.Dir(node.FilePath)
		}
		if key == "." {
			key = "root"
		}
		m[key] = append(m[key], node)
	}
	return m
}

func groupImportsByModule(graph *strategy.CodeGraph) map[string][]strategy.Import {
	m := map[string][]strategy.Import{}
	for _, imp := range graph.Imports {
		dir := filepath.Dir(imp.FilePath)
		if dir == "." {
			dir = "root"
		}
		m[dir] = append(m[dir], imp)
	}
	return m
}

func groupContentsByModule(contents []map[string]any) map[string][]map[string]any {
	m := map[string][]map[string]any{}
	for _, c := range contents {
		mod, _ := c["package"].(string)
		if mod == "" {
			if fileName, ok := c["fileName"].(string); ok {
				mod = filepath.Dir(fileName)
			}
		}
		if mod == "" || mod == "." {
			mod = "root"
		}
		m[mod] = append(m[mod], c)
	}
	return m
}
