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
	Overview           string            `json:"overview"`
	Architecture       ArchitectureInfo  `json:"architecture"`
	Patterns           PatternInfo       `json:"patterns"`
	DataFlow           DataFlowInfo      `json:"dataFlow"`
	QualityIndicators  QualityIndicators `json:"qualityIndicators"`
	KeyDataModels      string            `json:"keyDataModels"`      // 핵심 데이터 모델과 구조체 관계
	ModuleInteractions string            `json:"moduleInteractions"` // 모듈 간 호출/데이터 교환 맵
	CriticalPaths      string            `json:"criticalPaths"`      // end-to-end 핵심 실행 경로
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
	Module               string           `json:"module"`
	Language             string           `json:"language"`
	Summary              string           `json:"summary"`
	Functions            []FunctionDetail `json:"functions"`
	Types                []TypeDetail     `json:"types"`
	Variables            []VariableDetail `json:"variables"`
	Imports              []ImportDetail   `json:"imports"`
	Concurrency          string           `json:"concurrency"`
	Patterns             string           `json:"patterns"`
	Responsibilities     []string         `json:"responsibilities"`
	ExternalDeps         string           `json:"externalDeps"`
	Risks                string           `json:"risks"`
	Coupling             string           `json:"coupling"`             // 다른 모듈과의 결합도 분석
	TestNotes            string           `json:"testNotes"`            // 테스트 용이성, 주의사항
	CallFlow             string           `json:"callFlow"`             // 이 모듈의 전형적인 호출 흐름
	DataTransformations  string           `json:"dataTransformations"`  // 이 모듈에서 발생하는 데이터 변환
}

type FunctionDetail struct {
	Name        string        `json:"name"`
	Visibility  string        `json:"visibility"`  // exported / unexported
	Description string        `json:"description"`
	Parameters  []ParamDetail `json:"parameters,omitempty"`
	Returns     string        `json:"returns"`
	IsAsync     bool          `json:"isAsync"`
	Complexity  string        `json:"complexity"`  // low/medium/high + 한 줄 이유
	SideEffects string        `json:"sideEffects"`
	Calls       []string      `json:"calls,omitempty"`
	CalledBy    []string      `json:"calledBy,omitempty"`
}

type ParamDetail struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
}

type TypeDetail struct {
	Name        string        `json:"name"`
	Kind        string        `json:"kind"` // struct, interface, enum, type
	Description string        `json:"description"`
	Fields      []FieldDetail `json:"fields,omitempty"`
	Methods     []string      `json:"methods,omitempty"`    // 이 타입이 가진 메서드 목록
	Implements  []string      `json:"implements,omitempty"` // 구현하는 인터페이스 목록
	Embedded    []string      `json:"embedded,omitempty"`   // 임베드된 타입 목록
}

type FieldDetail struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
}

type VariableDetail struct {
	Name        string `json:"name"`
	Kind        string `json:"kind"` // variable, constant
	Description string `json:"description"`
	Scope       string `json:"scope"`
}

type ImportDetail struct {
	Path    string `json:"path"`
	Alias   string `json:"alias,omitempty"`
	Purpose string `json:"purpose"`
}

// moduleChunkInput은 청크 파일 형식입니다.
type moduleChunkInput struct {
	Language string            `json:"language"`
	Modules  []moduleChunkData `json:"modules"`
}

type moduleChunkData struct {
	Module   string            `json:"module"`
	Nodes    []strategy.Node   `json:"nodes"`
	Imports  []strategy.Import `json:"imports"`
	Contents []map[string]any  `json:"contents"`
}
