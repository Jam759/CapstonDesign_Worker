package projectContext

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"worker_GoVer/ai"
	"worker_GoVer/artifact/codeGraph/strategy"
	"worker_GoVer/db"
	"worker_GoVer/logger"
	"worker_GoVer/s3"
)

var log = logger.WithComponent("projectContext")

// GenerateProjectContext는 codeGraph + codeContent 기반으로 프로젝트 분석 문서를 생성합니다.
// 흐름: metrics 계산 → signals 정적 분석 → 모듈별 AI 분석(병렬) → 전체 AI 분석 + signals 보정 → 저장
func GenerateProjectContext(ctx context.Context, projectPath string, graphPath string, contentPath string, version int, project ProjectMetadata) (string, error) {
	log.Trace(ctx, "projectContext generation start", slog.Int("version", version))

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
	log.Trace(ctx, "graph loaded",
		slog.String("language", graph.Language),
		slog.Int("nodes", len(graph.Nodes)),
		slog.Int("edges", len(graph.Edges)),
		slog.Int("imports", len(graph.Imports)),
	)
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
	log.Trace(ctx, "starting chunk AI analysis",
		slog.Int("modules", len(moduleNames)),
		slog.Int("chunks", len(chunks)),
	)

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
			ds, err := analyzeModuleChunk(ctx, graph.Language, mods, moduleNodes, moduleImports, moduleContents)
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

	log.Trace(ctx, "module analysis done", slog.Int("modules", len(details)))

	// 5. 전체 프로젝트 분석 + signals AI 보정 (파일 전달)
	log.Trace(ctx, "generating overview")
	analysis, correctedSignals, err := generateOverview(ctx, details, &graph, metrics, signals, graphPath, contentPath, project)
	if err != nil {
		return "", fmt.Errorf("failed to generate overview: %w", err)
	}

	// AI 보정된 signals 적용
	if correctedSignals != nil {
		signals = *correctedSignals
	}

	// 6. ProjectContext 조립
	seoul, _ := time.LoadLocation("Asia/Seoul")
	pctx := ProjectContext{
		Project:       project,
		Metrics:       metrics,
		Signals:       signals,
		Analysis:      *analysis,
		ModuleDetails: details,
		CodeGraph:     &graph,
		GeneratedAt:   time.Now().In(seoul).Format(time.RFC3339),
	}

	// 7. 저장
	return saveProjectContext(ctx, projectPath, pctx, version, seoul)
}

// UpdateProjectContext는 baseline ProjectContext를 git diff 기반으로 증분 업데이트합니다.
// diffFiles가 없으면 CodeGraph만 교체하고 기존 분석 결과를 유지합니다.
func UpdateProjectContext(
	ctx context.Context,
	localPath string,
	baselineKBPath string,
	diffPath string,
	graphPath string,
	changedFilePaths []string,
	beforeCommit string,
	afterCommit string,
	version int,
	project ProjectMetadata,
) (string, error) {
	log.Trace(ctx, "projectContext incremental update start",
		slog.Int("version", version),
		slog.Int("changedFiles", len(changedFilePaths)),
	)

	// baseline 파싱
	baselineData, err := os.ReadFile(baselineKBPath)
	if err != nil {
		return "", fmt.Errorf("failed to read baseline KB: %w", err)
	}
	var baseline ProjectContext
	if err := json.Unmarshal(baselineData, &baseline); err != nil {
		return "", fmt.Errorf("failed to parse baseline KB: %w", err)
	}

	// 신규 CodeGraph 파싱
	graphData, err := os.ReadFile(graphPath)
	if err != nil {
		return "", fmt.Errorf("failed to read code graph: %w", err)
	}
	var graph strategy.CodeGraph
	if err := json.Unmarshal(graphData, &graph); err != nil {
		return "", fmt.Errorf("failed to parse code graph: %w", err)
	}

	seoul, _ := time.LoadLocation("Asia/Seoul")

	// diff가 없으면 CodeGraph만 교체하고 baseline 분석 유지
	if len(changedFilePaths) == 0 {
		log.Trace(ctx, "no changed files, reusing baseline analysis")
		baseline.Project = project
		baseline.CodeGraph = &graph
		baseline.GeneratedAt = time.Now().In(seoul).Format(time.RFC3339)
		return saveProjectContext(ctx, localPath, baseline, version, seoul)
	}

	// focused CodeGraph 생성 (변경된 파일에 속한 노드/엣지만)
	focused := buildFocusedCodeGraph(&graph, changedFilePaths)
	focusedPath, err := writeTempJSON("focusedGraph_*.json", focused)
	if err != nil {
		return "", fmt.Errorf("failed to write focused graph: %w", err)
	}
	defer os.Remove(focusedPath)

	// AI 증분 업데이트 (실제 커밋 해시를 프롬프트에 전달)
	p := ai.IncrementalProjectContextPrompt(beforeCommit, afterCommit, projectMetadataPromptJSON(project))
	result := <-ai.GenerateMessageWithFiles(ctx, p.User, p.System, []string{baselineKBPath, diffPath, focusedPath})
	if result.Err != nil {
		return "", fmt.Errorf("incremental AI analysis failed: %w", result.Err)
	}

	responseStr, ok := result.Data.(string)
	if !ok {
		return "", fmt.Errorf("unexpected AI response type")
	}
	responseStr = extractJSONObject(responseStr)

	var updated ProjectContext
	if err := json.Unmarshal([]byte(responseStr), &updated); err != nil {
		log.Warn(ctx, "failed to parse incremental response, falling back to baseline", err)
		baseline.Project = project
		baseline.CodeGraph = &graph
		baseline.GeneratedAt = time.Now().In(seoul).Format(time.RFC3339)
		return saveProjectContext(ctx, localPath, baseline, version, seoul)
	}

	// CodeGraph는 항상 새로 생성된 것으로 교체
	updated.Project = project
	updated.CodeGraph = &graph
	updated.GeneratedAt = time.Now().In(seoul).Format(time.RFC3339)

	return saveProjectContext(ctx, localPath, updated, version, seoul)
}

// buildFocusedCodeGraph는 변경된 파일에 속한 노드/엣지/임포트만 포함한 CodeGraph를 반환합니다.
func buildFocusedCodeGraph(graph *strategy.CodeGraph, changedFilePaths []string) strategy.CodeGraph {
	changedSet := make(map[string]struct{}, len(changedFilePaths))
	for _, p := range changedFilePaths {
		changedSet[p] = struct{}{}
	}

	focused := strategy.CodeGraph{Language: graph.Language}

	// 변경된 파일에 속한 노드 수집 + 노드 ID 셋 구성
	changedNodeIDs := make(map[string]struct{})
	for _, node := range graph.Nodes {
		if _, ok := changedSet[node.FilePath]; ok {
			focused.Nodes = append(focused.Nodes, node)
			changedNodeIDs[node.ID] = struct{}{}
		}
	}

	// From 또는 To가 변경된 노드에 해당하는 엣지 수집
	for _, edge := range graph.Edges {
		_, fromChanged := changedNodeIDs[edge.From]
		_, toChanged := changedNodeIDs[edge.To]
		if fromChanged || toChanged {
			focused.Edges = append(focused.Edges, edge)
		}
	}

	for _, imp := range graph.Imports {
		if _, ok := changedSet[imp.FilePath]; ok {
			focused.Imports = append(focused.Imports, imp)
		}
	}

	return focused
}

// saveProjectContext는 ProjectContext를 JSON으로 직렬화하여 artifact 디렉토리에 저장합니다.
func saveProjectContext(ctx context.Context, localPath string, pctx ProjectContext, version int, loc *time.Location) (string, error) {
	data, err := json.MarshalIndent(pctx, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal project context: %w", err)
	}

	artifactDir := filepath.Join(localPath, "artifact")
	if err := os.MkdirAll(artifactDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create artifact dir: %w", err)
	}

	fileName := fmt.Sprintf("projectContext_v%d_%s.json", version, time.Now().In(loc).Format("2006-01-02-15-04-05"))
	savePath := filepath.Join(artifactDir, fileName)

	if err := os.WriteFile(savePath, data, 0644); err != nil {
		return "", fmt.Errorf("failed to write project context: %w", err)
	}

	log.Trace(ctx, "projectContext saved", slog.String("path", savePath))
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

// analyzeModuleChunk는 청크에 포함된 모듈들을 파일 업로드 방식으로 AI 분석합니다.
func analyzeModuleChunk(ctx context.Context, language string, mods []string, moduleNodes map[string][]strategy.Node, moduleImports map[string][]strategy.Import, moduleContents map[string][]map[string]any) ([]ModuleDetail, error) {
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
	result := <-ai.GenerateMessageWithFiles(ctx, p.User, p.System, []string{chunkPath})
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
		// 파싱 실패 시 fallback
		details = make([]ModuleDetail, len(mods))
		for i, mod := range mods {
			details[i] = ModuleDetail{Module: mod, Language: language, Summary: responseStr}
		}
		return details, nil
	}

	for i := range details {
		if details[i].Module == "" && i < len(mods) {
			details[i].Module = mods[i]
		}
		details[i].Language = language
	}
	return details, nil
}

// 전체 프로젝트 분석 + signals 보정 (모든 데이터 파일로 전달)
func generateOverview(ctx context.Context, details []ModuleDetail, graph *strategy.CodeGraph, metrics Metrics, signals Signals, graphPath string, contentPath string, project ProjectMetadata) (*Analysis, *Signals, error) {
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

	p := ai.ProjectOverviewPrompt(projectMetadataPromptJSON(project))
	result := <-ai.GenerateMessageWithFiles(ctx, p.User, p.System, []string{graphPath, contentPath, detailsPath, metricsPath, signalsPath})
	if result.Err != nil {
		return nil, nil, result.Err
	}

	responseStr, ok := result.Data.(string)
	if !ok {
		return nil, nil, fmt.Errorf("unexpected response type")
	}

	responseStr = extractJSONObject(responseStr)
	responseStr = cleanTrailingCommas(responseStr)

	var rawParsed map[string]any
	if err := json.Unmarshal([]byte(responseStr), &rawParsed); err != nil {
		return &Analysis{Overview: responseStr}, nil, nil
	}

	analysisMap, ok := rawParsed["analysis"].(map[string]any)
	if !ok {
		return &Analysis{Overview: responseStr}, nil, nil
	}

	// LLM이 전체 응답 구조를 overview 필드 안에 넣는 경우 감지 후 언래핑
	if unwrapped := tryUnwrapNestedResponse(analysisMap); unwrapped != nil {
		rawParsed = unwrapped
		analysisMap = rawParsed["analysis"].(map[string]any)
	}

	analysis := mapToAnalysis(analysisMap)

	var correctedSignals *Signals
	if sigRaw, ok := rawParsed["signalCorrections"]; ok && sigRaw != nil {
		if sigBytes, err := json.Marshal(sigRaw); err == nil {
			var sig Signals
			if err := json.Unmarshal(sigBytes, &sig); err == nil {
				correctedSignals = &sig
			}
		}
	}

	return &analysis, correctedSignals, nil
}

// cleanTrailingCommas는 JSON에서 AI가 삽입하는 trailing comma를 제거합니다.
func projectMetadataPromptJSON(project ProjectMetadata) string {
	data, err := json.Marshal(project)
	if err != nil {
		return "{}"
	}
	return string(data)
}

var trailingCommaRe = regexp.MustCompile(`,\s*([}\]])`)

func cleanTrailingCommas(s string) string {
	return trailingCommaRe.ReplaceAllString(s, "$1")
}

// anyToString은 임의 값을 문자열로 변환합니다. 객체/배열은 JSON으로 직렬화합니다.
func anyToString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}

// mapToAnalysis는 map[string]any를 Analysis 구조체로 변환합니다.
// AI가 문자열 필드에 객체를 반환하는 경우에도 안전하게 처리합니다.
func mapToAnalysis(m map[string]any) Analysis {
	a := Analysis{
		Overview:           anyToString(m["overview"]),
		KeyDataModels:      anyToString(m["keyDataModels"]),
		ModuleInteractions: anyToString(m["moduleInteractions"]),
		CriticalPaths:      anyToString(m["criticalPaths"]),
	}
	if arch, ok := m["architecture"].(map[string]any); ok {
		a.Architecture = ArchitectureInfo{
			Summary:      anyToString(arch["summary"]),
			Layers:       anyToString(arch["layers"]),
			Dependencies: anyToString(arch["dependencies"]),
			EntryPoints:  anyToString(arch["entryPoints"]),
		}
	}
	if pat, ok := m["patterns"].(map[string]any); ok {
		a.Patterns = PatternInfo{
			Concurrency:         anyToString(pat["concurrency"]),
			DesignPatterns:      anyToString(pat["designPatterns"]),
			ErrorHandling:       anyToString(pat["errorHandling"]),
			ResourceManagement:  anyToString(pat["resourceManagement"]),
			Security:            anyToString(pat["security"]),
			ExternalIntegration: anyToString(pat["externalIntegration"]),
		}
	}
	if df, ok := m["dataFlow"].(map[string]any); ok {
		a.DataFlow = DataFlowInfo{
			Initialization:  anyToString(df["initialization"]),
			MainWorkflow:    anyToString(df["mainWorkflow"]),
			AsyncBoundaries: anyToString(df["asyncBoundaries"]),
			DataFormats:     anyToString(df["dataFormats"]),
		}
	}
	if qi, ok := m["qualityIndicators"].(map[string]any); ok {
		a.QualityIndicators = QualityIndicators{
			Strengths:       anyToString(qi["strengths"]),
			Risks:           anyToString(qi["risks"]),
			TechnicalDebt:   anyToString(qi["technicalDebt"]),
			Maintainability: anyToString(qi["maintainability"]),
		}
	}
	return a
}

// tryUnwrapNestedResponse는 LLM이 전체 응답 구조를 overview 필드 안에 넣었을 때 이를 감지하고 언래핑합니다.
// overview가 "analysis" 키를 포함하는 JSON 객체(또는 JSON 문자열)인 경우 해당 맵을 반환합니다.
func tryUnwrapNestedResponse(analysisMap map[string]any) map[string]any {
	overviewRaw := analysisMap["overview"]

	var overviewMap map[string]any
	switch v := overviewRaw.(type) {
	case map[string]any:
		overviewMap = v
	case string:
		if err := json.Unmarshal([]byte(strings.TrimSpace(v)), &overviewMap); err != nil {
			return nil
		}
	default:
		return nil
	}

	if _, hasAnalysis := overviewMap["analysis"]; hasAnalysis {
		return overviewMap
	}
	return nil
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
// 반환값: (삽입된 project_analysis_reports_id, S3 URL, error)
// S3 업로드 성공 후 DB 삽입이 실패한 경우에도 url은 반환하므로 호출자가 S3 오브젝트를 정리할 수 있습니다.
func Persist(ctx context.Context, filePath string, prevKBID *int64, installationID int64, repoID int64, projectID int64, version int, s3Bucket string, beforeCommit string, afterCommit string) (int64, string, error) {
	url, err := s3.UploadProjectContext(ctx, installationID, repoID, filePath)
	if err != nil {
		return 0, "", fmt.Errorf("projectContext S3 upload failed: %w", err)
	}

	var sizeBytes int64
	if info, err := os.Stat(filePath); err == nil {
		sizeBytes = info.Size()
	}

	id, err := db.InsertAnalysisReport(projectID, prevKBID, "PROJECT_KB", version, s3Bucket, url, sizeBytes, beforeCommit, afterCommit)
	if err != nil {
		return 0, url, fmt.Errorf("projectContext DB insert failed: %w", err)
	}
	log.Trace(ctx, "PROJECT_KB saved",
		slog.Int("version", version),
		slog.String("url", url),
	)
	return id, url, nil
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
	// filePath → package 매핑 (groupNodesByModule과 동일한 키 사용)
	fileToPackage := map[string]string{}
	for _, node := range graph.Nodes {
		if _, ok := fileToPackage[node.FilePath]; !ok {
			pkg := node.Package
			if pkg == "" {
				pkg = filepath.Dir(node.FilePath)
				if pkg == "." {
					pkg = "root"
				}
			}
			fileToPackage[node.FilePath] = pkg
		}
	}

	m := map[string][]strategy.Import{}
	for _, imp := range graph.Imports {
		key := fileToPackage[imp.FilePath]
		if key == "" {
			key = filepath.Dir(imp.FilePath)
			if key == "." {
				key = "root"
			}
		}
		m[key] = append(m[key], imp)
	}
	return m
}

func groupContentsByModule(contents []map[string]any) map[string][]map[string]any {
	m := map[string][]map[string]any{}
	for _, c := range contents {
		// "package" 필드 우선 (groupNodesByModule과 동일한 키)
		mod, _ := c["package"].(string)
		if mod == "" || mod == "." {
			mod = "root"
		}
		m[mod] = append(m[mod], c)
	}
	return m
}
