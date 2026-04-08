package projectContext

import (
	"strings"
	"worker_GoVer/artifact/codeGraph/strategy"
)

// CalculateSignals는 codeGraph + codeContent 기반으로 정적 분석 신호를 계산합니다.
// AI 보정 전 초기값입니다.
func CalculateSignals(graph *strategy.CodeGraph, contents []map[string]any) Signals {
	s := Signals{}

	// 모듈 경계 분리 여부: 3개 이상 모듈이면 분리된 것으로 판단
	modules := map[string]bool{}
	for _, node := range graph.Nodes {
		pkg := node.Package
		if pkg == "" {
			pkg = "root"
		}
		modules[pkg] = true
	}
	s.HasClearModuleBoundaries = len(modules) >= 3

	// 코드 내용 기반 키워드 검색
	allContent := collectAllContent(contents)

	// 오케스트레이션 레이어 여부 (facade, orchestrator, usecase, service 키워드)
	s.HasSeparatedOrchestrationLayer = containsAny(allContent,
		"facade", "Facade", "orchestrat", "Orchestrat", "usecase", "UseCase")

	// 런타임 검증 여부
	s.HasRuntimeValidation = containsAny(allContent,
		"validate", "Validate", "validator", "Validator",
		"@Valid", "assert", "Assert", "require", "Require")

	// 테스트 격리 이슈: 테스트 파일에서 외부 의존성 직접 사용
	s.HasTestingIsolationIssue = detectTestIsolationIssue(graph, contents)

	// 재시도 일관성 리스크: retry 키워드 존재하지만 일관된 정책 부재
	hasRetry := containsAny(allContent, "retry", "Retry", "retries", "backoff", "Backoff")
	hasIdempotent := containsAny(allContent, "idempoten", "Idempoten", "dedup", "Dedup", "deliveryId")
	s.HasRetryConsistencyRisk = hasRetry && !hasIdempotent

	// 워크스페이스 안전 제어
	s.HasWorkspaceSafetyControls = containsAny(allContent,
		"sandbox", "Sandbox", "isolation", "Isolation",
		"secureJoin", "SecureJoin", "chroot", "jail")

	// 에러 래핑
	s.HasErrorWrapping = containsAny(allContent,
		"fmt.Errorf", "%w", "errors.Wrap", "Wrap(", "wrap(")

	// Graceful shutdown
	s.HasGracefulShutdown = containsAny(allContent,
		"Shutdown", "shutdown", "signal.Notify", "SIGTERM", "SIGINT",
		"@PreDestroy", "addShutdownHook", "graceful")

	// 로깅
	s.HasLogging = containsAny(allContent,
		"log.", "Log.", "logger", "Logger", "slog.", "zap.", "logrus.",
		"@Slf4j", "LoggerFactory", "logging")

	// 인증
	s.HasAuthentication = containsAny(allContent,
		"auth", "Auth", "jwt", "JWT", "token", "Token",
		"Bearer", "OAuth", "oauth", "apiKey", "ApiKey")

	// 동시성 제어
	s.HasConcurrencyControl = containsAny(allContent,
		"sync.", "Mutex", "WaitGroup", "chan ", "<-chan", "goroutine",
		"synchronized", "Lock", "Semaphore", "atomic.",
		"async", "await", "Future", "Promise", "CompletableFuture")

	// 리소스 정리
	s.HasResourceCleanup = containsAny(allContent,
		"defer ", "finally", "close()", "Close()", ".close(",
		"try-with-resources", "using ", "@PreDestroy",
		"cleanup", "Cleanup", "dispose", "Dispose")

	return s
}

func collectAllContent(contents []map[string]any) string {
	var sb strings.Builder
	for _, c := range contents {
		if content, ok := c["content"].(string); ok {
			sb.WriteString(content)
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

func containsAny(text string, keywords ...string) bool {
	for _, kw := range keywords {
		if strings.Contains(text, kw) {
			return true
		}
	}
	return false
}

func detectTestIsolationIssue(graph *strategy.CodeGraph, contents []map[string]any) bool {
	// 테스트 파일이 있는지 확인
	hasTestFiles := false
	for _, node := range graph.Nodes {
		if strings.Contains(node.FilePath, "_test.") || strings.Contains(node.FilePath, "Test") {
			hasTestFiles = true
			break
		}
	}
	if !hasTestFiles {
		return false
	}

	// 테스트 파일에서 외부 의존성 직접 호출 여부
	for _, c := range contents {
		fileName, _ := c["fileName"].(string)
		content, _ := c["content"].(string)
		if !strings.Contains(fileName, "_test.") && !strings.Contains(fileName, "Test") {
			continue
		}
		// 테스트에서 실제 외부 호출 (mock 없이)
		if containsAny(content, "http.Get", "http.Post", "sql.Open", "net.Dial",
			"sqs.", "s3.", "dynamodb.") && !containsAny(content, "mock", "Mock", "stub", "Stub", "fake", "Fake") {
			return true
		}
	}
	return false
}
