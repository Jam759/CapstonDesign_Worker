package strategy

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

type GoStrategy struct{}

func (g GoStrategy) SupportedExtensions() []string {
	return []string{".go"}
}

func (g GoStrategy) Analyze(projectPath string) (*CodeGraph, error) {
	graph := &CodeGraph{
		Language: "go",
	}

	fset := token.NewFileSet()

	err := filepath.Walk(projectPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			// vendor, .git, artifact 제외
			name := info.Name()
			if name == "vendor" || name == ".git" || name == "artifact" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		relPath, _ := filepath.Rel(projectPath, path)
		f, err := parser.ParseFile(fset, path, nil, parser.AllErrors)
		if err != nil {
			return nil // 파싱 실패한 파일은 스킵
		}

		pkgName := f.Name.Name

		// imports
		for _, imp := range f.Imports {
			importPath := strings.Trim(imp.Path.Value, `"`)
			alias := ""
			if imp.Name != nil {
				alias = imp.Name.Name
			}
			graph.Imports = append(graph.Imports, Import{
				FilePath:   relPath,
				ImportPath: importPath,
				Alias:      alias,
			})
		}

		// AST 순회
		ast.Inspect(f, func(n ast.Node) bool {
			switch decl := n.(type) {
			case *ast.FuncDecl:
				g.extractFunction(graph, decl, fset, relPath, pkgName)
			case *ast.GenDecl:
				g.extractGenDecl(graph, decl, fset, relPath, pkgName)
			}
			return true
		})

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to walk project: %w", err)
	}

	return graph, nil
}

func (g GoStrategy) extractFunction(graph *CodeGraph, decl *ast.FuncDecl, fset *token.FileSet, filePath, pkgName string) {
	receiver := ""
	kind := "function"

	if decl.Recv != nil && len(decl.Recv.List) > 0 {
		kind = "method"
		receiver = g.typeString(decl.Recv.List[0].Type)
	}

	nodeID := g.makeNodeID(pkgName, receiver, decl.Name.Name)
	line := fset.Position(decl.Pos()).Line

	endLine := fset.Position(decl.End()).Line

	graph.Nodes = append(graph.Nodes, Node{
		ID:       nodeID,
		Name:     decl.Name.Name,
		Kind:     kind,
		FilePath: filePath,
		Line:     line,
		EndLine:  endLine,
		Package:  pkgName,
		Receiver: receiver,
	})

	// 함수 본문에서 호출 관계 추출
	if decl.Body != nil {
		ast.Inspect(decl.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			callee := g.calleeString(call)
			if callee != "" {
				graph.Edges = append(graph.Edges, Edge{
					From:     nodeID,
					To:       callee,
					Relation: "calls",
				})
			}
			return true
		})
	}
}

func (g GoStrategy) extractGenDecl(graph *CodeGraph, decl *ast.GenDecl, fset *token.FileSet, filePath, pkgName string) {
	for _, spec := range decl.Specs {
		switch s := spec.(type) {
		case *ast.TypeSpec:
			kind := "struct"
			switch s.Type.(type) {
			case *ast.InterfaceType:
				kind = "interface"
			case *ast.StructType:
				kind = "struct"
			default:
				kind = "type"
			}

			nodeID := fmt.Sprintf("%s.%s", pkgName, s.Name.Name)
			line := fset.Position(s.Pos()).Line
			endLine := fset.Position(s.End()).Line

			graph.Nodes = append(graph.Nodes, Node{
				ID:       nodeID,
				Name:     s.Name.Name,
				Kind:     kind,
				FilePath: filePath,
				Line:     line,
				EndLine:  endLine,
				Package:  pkgName,
			})

			// struct의 임베딩 관계 추출
			if st, ok := s.Type.(*ast.StructType); ok {
				for _, field := range st.Fields.List {
					if len(field.Names) == 0 { // 임베디드 필드
						embeddedType := g.typeString(field.Type)
						graph.Edges = append(graph.Edges, Edge{
							From:     nodeID,
							To:       embeddedType,
							Relation: "embeds",
						})
					}
				}
			}

		case *ast.ValueSpec:
			kind := "variable"
			if decl.Tok == token.CONST {
				kind = "constant"
			}

			for _, name := range s.Names {
				if name.Name == "_" {
					continue
				}
				nodeID := fmt.Sprintf("%s.%s", pkgName, name.Name)
				line := fset.Position(name.Pos()).Line
				endLine := fset.Position(s.End()).Line

				graph.Nodes = append(graph.Nodes, Node{
					ID:       nodeID,
					Name:     name.Name,
					Kind:     kind,
					FilePath: filePath,
					Line:     line,
					EndLine:  endLine,
					Package:  pkgName,
				})
			}
		}
	}
}

func (g GoStrategy) makeNodeID(pkgName, receiver, funcName string) string {
	if receiver != "" {
		return fmt.Sprintf("%s.%s.%s", pkgName, receiver, funcName)
	}
	return fmt.Sprintf("%s.%s", pkgName, funcName)
}

func (g GoStrategy) typeString(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return g.typeString(t.X)
	case *ast.SelectorExpr:
		return g.typeString(t.X) + "." + t.Sel.Name
	default:
		return ""
	}
}

func (g GoStrategy) calleeString(call *ast.CallExpr) string {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return fn.Name
	case *ast.SelectorExpr:
		return g.typeString(fn.X) + "." + fn.Sel.Name
	default:
		return ""
	}
}
