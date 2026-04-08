package strategy

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	reClass      = regexp.MustCompile(`^(\s*)class\s+(\w+)`)
	reFunc       = regexp.MustCompile(`^(\s*)def\s+(\w+)\s*\(`)
	reImport     = regexp.MustCompile(`^\s*import\s+(.+)`)
	reFromImport = regexp.MustCompile(`^\s*from\s+(\S+)\s+import\s+(.+)`)
	reCall       = regexp.MustCompile(`(\w[\w.]*)\s*\(`)
)

type PythonStrategy struct{}

func (p PythonStrategy) SupportedExtensions() []string {
	return []string{".py"}
}

func (p PythonStrategy) Analyze(projectPath string) (*CodeGraph, error) {
	graph := &CodeGraph{Language: "python"}
	log.Printf("[PyStrategy] start analyzing: %s", projectPath)

	fileCount := 0
	err := filepath.Walk(projectPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			name := info.Name()
			if name == ".git" || name == "artifact" || name == "__pycache__" ||
				name == ".venv" || name == "venv" || name == "node_modules" {
				log.Printf("[PyStrategy] skip dir: %s", path)
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".py") {
			return nil
		}
		relPath, _ := filepath.Rel(projectPath, path)
		log.Printf("[PyStrategy] parsing: %s", relPath)
		p.parseFile(graph, path, relPath)
		fileCount++
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to walk project: %w", err)
	}

	log.Printf("[PyStrategy] done: files=%d nodes=%d edges=%d imports=%d",
		fileCount, len(graph.Nodes), len(graph.Edges), len(graph.Imports))
	return graph, nil
}

type classFrame struct {
	name   string
	indent int
}

func (p PythonStrategy) parseFile(graph *CodeGraph, absPath, relPath string) {
	f, err := os.Open(absPath)
	if err != nil {
		log.Printf("[PyStrategy] failed to open file %s: %v", relPath, err)
		return
	}
	defer f.Close()

	modName := p.moduleName(relPath)

	// 현재 함수 노드 (call edge 추출용)
	type funcFrame struct {
		nodeID  string
		indent  int
		bodyLines []string
	}

	var classStack []classFrame
	var funcStack []funcFrame

	scanner := bufio.NewScanner(f)
	lineNum := 0
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	// 1패스: node + import 수집
	for i, line := range lines {
		lineNum = i + 1
		indent := p.indentLevel(line)

		// 클래스 스택 정리
		for len(classStack) > 0 && indent <= classStack[len(classStack)-1].indent {
			classStack = classStack[:len(classStack)-1]
		}
		// 함수 스택 정리
		for len(funcStack) > 0 && indent <= funcStack[len(funcStack)-1].indent {
			funcStack = funcStack[:len(funcStack)-1]
		}

		// import
		if m := reFromImport.FindStringSubmatch(line); m != nil {
			module := strings.TrimSpace(m[1])
			names := strings.Split(m[2], ",")
			for _, name := range names {
				name = strings.TrimSpace(name)
				if alias := strings.Fields(name); len(alias) == 3 && alias[1] == "as" {
					graph.Imports = append(graph.Imports, Import{
						FilePath:   relPath,
						ImportPath: module + "." + alias[0],
						Alias:      alias[2],
					})
				} else {
					graph.Imports = append(graph.Imports, Import{
						FilePath:   relPath,
						ImportPath: module + "." + strings.Fields(name)[0],
					})
				}
			}
			continue
		}
		if m := reImport.FindStringSubmatch(line); m != nil {
			for _, part := range strings.Split(m[1], ",") {
				part = strings.TrimSpace(part)
				fields := strings.Fields(part)
				if len(fields) == 3 && fields[1] == "as" {
					graph.Imports = append(graph.Imports, Import{
						FilePath:   relPath,
						ImportPath: fields[0],
						Alias:      fields[2],
					})
				} else if len(fields) >= 1 {
					graph.Imports = append(graph.Imports, Import{
						FilePath:   relPath,
						ImportPath: fields[0],
					})
				}
			}
			continue
		}

		// class
		if m := reClass.FindStringSubmatch(line); m != nil {
			className := m[2]
			log.Printf("[PyStrategy] class: %s (line %d) in %s", className, lineNum, relPath)
			nodeID := fmt.Sprintf("%s.%s", modName, className)
			// 부모 클래스 implements edge
			if idx := strings.Index(line, "("); idx != -1 {
				end := strings.Index(line, ")")
				if end > idx {
					parents := strings.Split(line[idx+1:end], ",")
					for _, parent := range parents {
						parent = strings.TrimSpace(parent)
						if parent != "" && parent != "object" {
							graph.Edges = append(graph.Edges, Edge{
								From:     nodeID,
								To:       parent,
								Relation: "implements",
							})
						}
					}
				}
			}
			endLine := p.findBlockEnd(lines, i)
			graph.Nodes = append(graph.Nodes, Node{
				ID:       nodeID,
				Name:     className,
				Kind:     "class",
				FilePath: relPath,
				Line:     lineNum,
				EndLine:  endLine,
				Package:  modName,
			})
			classStack = append(classStack, classFrame{name: className, indent: indent})
			continue
		}

		// def
		if m := reFunc.FindStringSubmatch(line); m != nil {
			funcName := m[2]
			log.Printf("[PyStrategy] func: %s (line %d) in %s", funcName, lineNum, relPath)
			kind := "function"
			receiver := ""
			var nodeID string

			if len(classStack) > 0 {
				kind = "method"
				receiver = classStack[len(classStack)-1].name
				nodeID = fmt.Sprintf("%s.%s.%s", modName, receiver, funcName)
			} else {
				nodeID = fmt.Sprintf("%s.%s", modName, funcName)
			}

			endLine := p.findBlockEnd(lines, i)
			graph.Nodes = append(graph.Nodes, Node{
				ID:       nodeID,
				Name:     funcName,
				Kind:     kind,
				FilePath: relPath,
				Line:     lineNum,
				EndLine:  endLine,
				Package:  modName,
				Receiver: receiver,
			})
			funcStack = append(funcStack, funcFrame{nodeID: nodeID, indent: indent})
			continue
		}

		// call edges (현재 함수 body 안)
		if len(funcStack) > 0 {
			callerID := funcStack[len(funcStack)-1].nodeID
			matches := reCall.FindAllStringSubmatch(line, -1)
			for _, cm := range matches {
				callee := cm[1]
				// 키워드, 빌트인 제외
				if p.isBuiltin(callee) {
					continue
				}
				graph.Edges = append(graph.Edges, Edge{
					From:     callerID,
					To:       callee,
					Relation: "calls",
				})
			}
		}
	}
}

// moduleName은 상대 경로를 모듈 이름으로 변환합니다 (예: "api/views.py" → "api.views")
func (p PythonStrategy) moduleName(relPath string) string {
	name := strings.TrimSuffix(relPath, ".py")
	name = strings.ReplaceAll(name, string(filepath.Separator), ".")
	name = strings.ReplaceAll(name, "/", ".")
	return name
}

// indentLevel은 줄의 앞 공백 수를 반환합니다 (tab=4)
func (p PythonStrategy) indentLevel(line string) int {
	count := 0
	for _, ch := range line {
		if ch == ' ' {
			count++
		} else if ch == '\t' {
			count += 4
		} else {
			break
		}
	}
	return count
}

// findBlockEnd는 블록의 마지막 줄 번호를 반환합니다
func (p PythonStrategy) findBlockEnd(lines []string, startIdx int) int {
	baseIndent := p.indentLevel(lines[startIdx])
	for i := startIdx + 1; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line) == "" {
			continue
		}
		if p.indentLevel(line) <= baseIndent {
			return i // 1-indexed이므로 i (exclusive)
		}
	}
	return len(lines)
}

var builtins = map[string]bool{
	"print": true, "len": true, "range": true, "enumerate": true, "zip": true,
	"map": true, "filter": true, "list": true, "dict": true, "set": true,
	"tuple": true, "str": true, "int": true, "float": true, "bool": true,
	"isinstance": true, "issubclass": true, "hasattr": true, "getattr": true,
	"setattr": true, "super": true, "type": true, "open": true, "input": true,
	"abs": true, "max": true, "min": true, "sum": true, "sorted": true,
	"reversed": true, "any": true, "all": true, "next": true, "iter": true,
	"if": true, "for": true, "while": true, "return": true, "yield": true,
	"raise": true, "assert": true, "with": true, "except": true,
}

func (p PythonStrategy) isBuiltin(name string) bool {
	base := strings.SplitN(name, ".", 2)[0]
	return builtins[base]
}
