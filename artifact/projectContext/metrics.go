package projectContext

import (
	"worker_GoVer/artifact/codeGraph/strategy"
)

// CalculateMetrics는 codeGraph에서 정량 메트릭을 계산합니다.
func CalculateMetrics(graph *strategy.CodeGraph) Metrics {
	m := Metrics{}

	// 모듈(패키지) 수
	modules := map[string]bool{}
	files := map[string]bool{}

	for _, node := range graph.Nodes {
		pkg := node.Package
		if pkg == "" {
			pkg = "root"
		}
		modules[pkg] = true
		files[node.FilePath] = true

		m.CodeUnitCount++

		switch node.Kind {
		case "function":
			m.FunctionCount++
		case "method":
			m.MethodCount++
		case "struct":
			m.TypeCount++
		case "interface":
			m.InterfaceCount++
		case "variable":
			m.VariableCount++
		case "constant":
			m.ConstantCount++
		case "type":
			m.TypeCount++
		}
	}

	m.ModuleCount = len(modules)
	m.FileCount = len(files)

	// 의존성 분석
	externalDeps := map[string]bool{}
	internalDeps := map[string]bool{}
	for _, imp := range graph.Imports {
		if isExternalImport(imp.ImportPath, graph.Language) {
			externalDeps[imp.ImportPath] = true
		} else {
			internalDeps[imp.ImportPath] = true
		}
	}
	m.ExternalDependencyCount = len(externalDeps)
	m.InternalDependencyCount = len(internalDeps)

	// 호출 관계 분석
	callGraph := buildCallGraph(graph.Edges)
	m.MaxCallDepth = calcMaxCallDepth(callGraph)
	m.AvgCallDepth = calcAvgCallDepth(callGraph)
	m.CircularDependencyCount = countCircularDeps(callGraph)

	// 비동기 흐름 카운트 (channel, goroutine, async 관련 호출)
	m.AsyncFlowCount = countAsyncFlows(graph)

	return m
}

func isExternalImport(importPath string, language string) bool {
	// Go: 표준 라이브러리가 아니고 모듈 접두사가 아닌 것
	// 단순 휴리스틱: "."이 포함되면 외부 (예: github.com/...)
	for _, c := range importPath {
		if c == '.' {
			return true
		}
	}
	return false
}

func buildCallGraph(edges []strategy.Edge) map[string][]string {
	g := map[string][]string{}
	for _, e := range edges {
		if e.Relation == "calls" {
			g[e.From] = append(g[e.From], e.To)
		}
	}
	return g
}

func calcMaxCallDepth(callGraph map[string][]string) int {
	maxDepth := 0
	visited := map[string]bool{}

	var dfs func(node string, depth int)
	dfs = func(node string, depth int) {
		if visited[node] {
			return
		}
		visited[node] = true
		if depth > maxDepth {
			maxDepth = depth
		}
		for _, next := range callGraph[node] {
			dfs(next, depth+1)
		}
		visited[node] = false
	}

	for node := range callGraph {
		dfs(node, 0)
	}
	return maxDepth
}

func calcAvgCallDepth(callGraph map[string][]string) float64 {
	if len(callGraph) == 0 {
		return 0
	}
	totalDepth := 0
	for _, targets := range callGraph {
		totalDepth += len(targets)
	}
	return float64(totalDepth) / float64(len(callGraph))
}

func countCircularDeps(callGraph map[string][]string) int {
	count := 0
	visited := map[string]int{} // 0=unvisited, 1=in-stack, 2=done

	var dfs func(node string) bool
	dfs = func(node string) bool {
		if visited[node] == 1 {
			return true // cycle
		}
		if visited[node] == 2 {
			return false
		}
		visited[node] = 1
		for _, next := range callGraph[node] {
			if dfs(next) {
				count++
			}
		}
		visited[node] = 2
		return false
	}

	for node := range callGraph {
		if visited[node] == 0 {
			dfs(node)
		}
	}
	return count
}

func countAsyncFlows(graph *strategy.CodeGraph) int {
	asyncKeywords := map[string]bool{
		"go":              true,
		"make":            true, // make(chan ...)
		"sync.WaitGroup":  true,
		"sync.Mutex":      true,
		"sync.RWMutex":    true,
		"async":           true,
		"await":           true,
		"Future":          true,
		"Promise":         true,
		"CompletableFuture": true,
		"ExecutorService": true,
		"Thread":          true,
	}

	count := 0
	for _, edge := range graph.Edges {
		if edge.Relation == "calls" && asyncKeywords[edge.To] {
			count++
		}
	}
	return count
}
