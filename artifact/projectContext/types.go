package projectContext

import "worker_GoVer/artifact/codeGraph/strategy"

// ProjectContext는 프로젝트 전반의 분석 문서
type ProjectContext struct {
	Metrics       Metrics             `json:"metrics"`
	Signals       Signals             `json:"signals"`
	Analysis      Analysis            `json:"analysis"`
	ModuleDetails []ModuleDetail      `json:"moduleDetails"`
	CodeGraph     *strategy.CodeGraph `json:"codeGraph"`
	GeneratedAt   string              `json:"generatedAt"`
}

// Metrics는 codeGraph에서 계산된 정량 메트릭
type Metrics struct {
	ModuleCount             int     `json:"moduleCount"`
	CodeUnitCount           int     `json:"codeUnitCount"`
	FunctionCount           int     `json:"functionCount"`
	MethodCount             int     `json:"methodCount"`
	TypeCount               int     `json:"typeCount"`
	InterfaceCount          int     `json:"interfaceCount"`
	VariableCount           int     `json:"variableCount"`
	ConstantCount           int     `json:"constantCount"`
	AsyncFlowCount          int     `json:"asyncFlowCount"`
	ExternalDependencyCount int     `json:"externalDependencyCount"`
	InternalDependencyCount int     `json:"internalDependencyCount"`
	MaxCallDepth            int     `json:"maxCallDepth"`
	AvgCallDepth            float64 `json:"avgCallDepth"`
	CircularDependencyCount int     `json:"circularDependencyCount"`
	FileCount               int     `json:"fileCount"`
}

// Signals는 정적 분석 초기값 + AI 보정된 불리언 신호
type Signals struct {
	HasClearModuleBoundaries      bool `json:"hasClearModuleBoundaries"`
	HasSeparatedOrchestrationLayer bool `json:"hasSeparatedOrchestrationLayer"`
	HasRuntimeValidation          bool `json:"hasRuntimeValidation"`
	HasTestingIsolationIssue      bool `json:"hasTestingIsolationIssue"`
	HasRetryConsistencyRisk       bool `json:"hasRetryConsistencyRisk"`
	HasWorkspaceSafetyControls    bool `json:"hasWorkspaceSafetyControls"`
	HasErrorWrapping              bool `json:"hasErrorWrapping"`
	HasGracefulShutdown           bool `json:"hasGracefulShutdown"`
	HasLogging                    bool `json:"hasLogging"`
	HasAuthentication             bool `json:"hasAuthentication"`
	HasConcurrencyControl         bool `json:"hasConcurrencyControl"`
	HasResourceCleanup            bool `json:"hasResourceCleanup"`
}

// Analysis는 AI가 생성한 정성적 분석
type Analysis struct {
	Overview          string            `json:"overview"`
	Architecture      ArchitectureInfo  `json:"architecture"`
	Patterns          PatternInfo       `json:"patterns"`
	DataFlow          DataFlowInfo      `json:"dataFlow"`
	QualityIndicators QualityIndicators `json:"qualityIndicators"`
}

type ArchitectureInfo struct {
	Summary      string `json:"summary"`
	Layers       string `json:"layers"`
	Dependencies string `json:"dependencies"`
	EntryPoints  string `json:"entryPoints"`
}

type PatternInfo struct {
	Concurrency         string `json:"concurrency"`
	DesignPatterns      string `json:"designPatterns"`
	ErrorHandling       string `json:"errorHandling"`
	ResourceManagement  string `json:"resourceManagement"`
	Security            string `json:"security"`
	ExternalIntegration string `json:"externalIntegration"`
}

type DataFlowInfo struct {
	Initialization  string `json:"initialization"`
	MainWorkflow    string `json:"mainWorkflow"`
	AsyncBoundaries string `json:"asyncBoundaries"`
	DataFormats     string `json:"dataFormats"`
}

type QualityIndicators struct {
	Strengths       string `json:"strengths"`
	Risks           string `json:"risks"`
	TechnicalDebt   string `json:"technicalDebt"`
	Maintainability string `json:"maintainability"`
}

// ModuleDetail은 모듈별 상세 정보
type ModuleDetail struct {
	Module           string `json:"module"`
	Language         string `json:"language"`
	Summary          string `json:"summary"`
	Functions        string `json:"functions"`
	Types            string `json:"types"`
	Variables        string `json:"variables"`
	Imports          string `json:"imports"`
	Concurrency      string `json:"concurrency"`
	Patterns         string `json:"patterns"`
	Responsibilities string `json:"responsibilities"`
	ExternalDeps     string `json:"externalDeps"`
	Risks            string `json:"risks"`
}
