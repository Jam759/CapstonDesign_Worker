package codeContent

// CodeContent는 코드 그래프의 노드와 실제 소스코드를 매칭한 결과
type CodeContent struct {
	Type     string `json:"type"`
	Name     string `json:"name"`
	FileName string `json:"fileName"`
	Package  string `json:"package,omitempty"`
	Receiver string `json:"receiver,omitempty"`
	Content  string `json:"content"`
}
