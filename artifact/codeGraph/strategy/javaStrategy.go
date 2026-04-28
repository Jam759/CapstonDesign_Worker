package strategy

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	reJavaPackage    = regexp.MustCompile(`^\s*package\s+([\w.]+)\s*;`)
	reJavaImport     = regexp.MustCompile(`^\s*import\s+(?:static\s+)?([\w.]+(?:\.\*)?)\s*;`)
	reJavaClassDecl  = regexp.MustCompile(`\b(?:class|interface|enum|record|@interface)\s+(\w+)`)
	reJavaMethodDecl = regexp.MustCompile(`^\s*(?:(?:public|private|protected|static|final|abstract|synchronized|native|default|override|transient|volatile)\s+)*(?:<[\w,\s?]+>\s+)?(?:[\w<>\[\].,?]+)\s+(\w+)\s*\(`)
	reJavaExtends    = regexp.MustCompile(`\bextends\s+([\w.]+)`)
	reJavaImplements = regexp.MustCompile(`\bimplements\s+([\w.,\s]+?)(?:\{|$)`)
	reJavaCall       = regexp.MustCompile(`\b(\w[\w.]*)\s*\(`)
)

var javaKeywords = map[string]bool{
	"if": true, "else": true, "for": true, "while": true, "do": true,
	"switch": true, "case": true, "try": true, "catch": true, "finally": true,
	"return": true, "throw": true, "new": true, "instanceof": true,
	"assert": true, "synchronized": true, "super": true, "this": true,
	"void": true, "int": true, "long": true, "double": true, "float": true,
	"boolean": true, "char": true, "byte": true, "short": true,
	"String": true, "Object": true, "class": true, "interface": true,
	"enum": true, "record": true,
}

func (j JavaStrategy) SupportedExtensions() []string {
	return []string{".java"}
}

func (j JavaStrategy) Analyze(ctx context.Context, projectPath string) (*CodeGraph, error) {
	graph := &CodeGraph{Language: "java"}
	log.Trace(ctx, "java analysis start", slog.String("path", projectPath))

	fileCount := 0
	err := filepath.Walk(projectPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			name := info.Name()
			if name == ".git" || name == "artifact" || name == "build" ||
				name == "target" || name == ".gradle" || name == "node_modules" {
				log.Trace(ctx, "java skip dir", slog.String("path", path))
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".java") {
			return nil
		}
		relPath, _ := filepath.Rel(projectPath, path)
		log.Trace(ctx, "java parsing file", slog.String("file", relPath))
		j.parseFile(ctx, graph, path, relPath)
		fileCount++
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to walk project: %w", err)
	}

	log.Trace(ctx, "java analysis done",
		slog.Int("files", fileCount),
		slog.Int("nodes", len(graph.Nodes)),
		slog.Int("edges", len(graph.Edges)),
		slog.Int("imports", len(graph.Imports)),
	)
	return graph, nil
}

type javaClassCtx struct {
	name      string
	nodeID    string
	bodyDepth int
}

func (j JavaStrategy) parseFile(ctx context.Context, graph *CodeGraph, absPath, relPath string) {
	f, err := os.Open(absPath)
	if err != nil {
		log.Warn(ctx, "java failed to open file", err, slog.String("file", relPath))
		return
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	pkgName := ""
	depth := 0
	var classStack []javaClassCtx
	currentMethodID := ""
	currentMethodDepth := -1
	inBlockComment := false

	for i, line := range lines {
		lineNum := i + 1
		trimmed := strings.TrimSpace(line)

		if inBlockComment {
			if strings.Contains(line, "*/") {
				inBlockComment = false
			}
			continue
		}
		if strings.Contains(line, "/*") && !strings.Contains(line, "*/") {
			inBlockComment = true
		}
		if strings.HasPrefix(trimmed, "//") {
			continue
		}

		startDepth := depth
		inStr := false
		inChar := false
		for k := 0; k < len(line); k++ {
			ch := line[k]
			if ch == '"' && !inChar {
				inStr = !inStr
			} else if ch == '\'' && !inStr {
				inChar = !inChar
			} else if !inStr && !inChar {
				if ch == '{' {
					depth++
				} else if ch == '}' {
					depth--
				}
			}
		}

		for len(classStack) > 0 && depth < classStack[len(classStack)-1].bodyDepth {
			classStack = classStack[:len(classStack)-1]
		}
		if currentMethodDepth >= 0 && depth <= currentMethodDepth {
			currentMethodID = ""
			currentMethodDepth = -1
		}

		if pkgName == "" {
			if m := reJavaPackage.FindStringSubmatch(line); m != nil {
				pkgName = m[1]
				continue
			}
		}

		if m := reJavaImport.FindStringSubmatch(line); m != nil {
			graph.Imports = append(graph.Imports, Import{
				FilePath:   relPath,
				ImportPath: m[1],
			})
			continue
		}

		if m := reJavaClassDecl.FindStringSubmatch(line); m != nil {
			className := m[1]
			if javaKeywords[className] {
				goto callCheck
			}
			var nodeID string
			if len(classStack) > 0 {
				nodeID = classStack[len(classStack)-1].nodeID + "." + className
			} else {
				if pkgName != "" {
					nodeID = pkgName + "." + className
				} else {
					nodeID = className
				}
			}

			kind := "class"
			if strings.Contains(line, "interface") {
				kind = "interface"
			} else if strings.Contains(line, "enum") {
				kind = "enum"
			} else if strings.Contains(line, "record") {
				kind = "record"
			}

			log.Trace(ctx, "java found class",
				slog.String("kind", kind),
				slog.String("class", className),
				slog.Int("line", lineNum),
				slog.String("file", relPath),
			)

			graph.Nodes = append(graph.Nodes, Node{
				ID:       nodeID,
				Name:     className,
				Kind:     kind,
				FilePath: relPath,
				Line:     lineNum,
				EndLine:  lineNum,
				Package:  pkgName,
			})

			if em := reJavaExtends.FindStringSubmatch(line); em != nil {
				graph.Edges = append(graph.Edges, Edge{
					From:     nodeID,
					To:       em[1],
					Relation: "implements",
				})
			}
			if im := reJavaImplements.FindStringSubmatch(line); im != nil {
				for _, iface := range strings.Split(im[1], ",") {
					iface = strings.TrimSpace(iface)
					if iface != "" {
						graph.Edges = append(graph.Edges, Edge{
							From:     nodeID,
							To:       iface,
							Relation: "implements",
						})
					}
				}
			}

			classStack = append(classStack, javaClassCtx{
				name:      className,
				nodeID:    nodeID,
				bodyDepth: depth,
			})
			continue
		}

		if len(classStack) > 0 && startDepth == classStack[len(classStack)-1].bodyDepth {
			if m := reJavaMethodDecl.FindStringSubmatch(line); m != nil {
				methodName := m[1]
				if !javaKeywords[methodName] {
					classCtx := classStack[len(classStack)-1]
					nodeID := classCtx.nodeID + "." + methodName

					kind := "method"
					if strings.Contains(line, "static ") {
						kind = "function"
					}

					log.Trace(ctx, "java found method",
						slog.String("method", methodName),
						slog.Int("line", lineNum),
						slog.String("file", relPath),
					)

					graph.Nodes = append(graph.Nodes, Node{
						ID:       nodeID,
						Name:     methodName,
						Kind:     kind,
						FilePath: relPath,
						Line:     lineNum,
						EndLine:  lineNum,
						Package:  pkgName,
						Receiver: classCtx.name,
					})

					if strings.Contains(line, "{") {
						currentMethodID = nodeID
						currentMethodDepth = depth
					}
					continue
				}
			}
		}

	callCheck:
		if currentMethodID != "" {
			matches := reJavaCall.FindAllStringSubmatch(line, -1)
			for _, cm := range matches {
				callee := cm[1]
				base := strings.SplitN(callee, ".", 2)[0]
				if !javaKeywords[base] && callee != currentMethodID {
					graph.Edges = append(graph.Edges, Edge{
						From:     currentMethodID,
						To:       callee,
						Relation: "calls",
					})
				}
			}
		}
	}
}
