package strategy

// Node는 코드 그래프의 노드 (함수, 구조체, 변수, 상수 등)
type Node struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Kind     string `json:"kind"` // "function", "method", "struct", "interface", "variable", "constant"
	FilePath string `json:"filePath"`
	Line     int    `json:"line"`
	EndLine  int    `json:"endLine"`
	Package  string `json:"package,omitempty"`
	Receiver string `json:"receiver,omitempty"` // 메서드의 리시버 타입
}

// Edge는 노드 간의 관계
type Edge struct {
	From     string `json:"from"`     // source node ID
	To       string `json:"to"`       // target node ID
	Relation string `json:"relation"` // "calls", "imports", "implements", "embeds"
}

// Import는 파일의 import 정보
type Import struct {
	FilePath   string `json:"filePath"`
	ImportPath string `json:"importPath"`
	Alias      string `json:"alias,omitempty"`
}

// CodeGraph는 코드 분석 결과
type CodeGraph struct {
	Language string   `json:"language"`
	Nodes    []Node   `json:"nodes"`
	Edges    []Edge   `json:"edges"`
	Imports  []Import `json:"imports"`
}

// CodeGraphStrategy는 언어별 코드 그래프 생성 전략 인터페이스
type CodeGraphStrategy interface {
	SupportedExtensions() []string
	Analyze(projectPath string) (*CodeGraph, error)
}
