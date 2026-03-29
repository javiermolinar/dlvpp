package main

import (
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"dlvpp/internal/backend"
)

type packageKey struct {
	Target       string
	IncludeTests bool
}

type packageEntry struct {
	Dir   string
	Files []string
}

type fileEntry struct {
	ModTime time.Time
	Size    int64
	Info    sourceFileInfo
}

type sourceFileInfo struct {
	Path    string
	Package string
	Symbols []symbolInfo
	Markers []breakpointMarker
}

type symbolInfo struct {
	Name      string
	Qualified string
	Kind      symbolKind
	Receiver  string
	Line      int
	IsTest    bool
}

type symbolKind string

const (
	symbolFunction symbolKind = "function"
	symbolMethod   symbolKind = "method"
)

type breakpointMarker struct {
	Line       int
	TargetLine int
	Raw        string
}

type sourceIndex struct {
	mu       sync.Mutex
	packages map[packageKey]packageEntry
	modules  map[string][]string
	files    map[string]fileEntry
}

func newSourceIndex() *sourceIndex {
	return &sourceIndex{
		packages: make(map[packageKey]packageEntry),
		modules:  make(map[string][]string),
		files:    make(map[string]fileEntry),
	}
}

func (idx *sourceIndex) PackageFiles(ctx context.Context, target string, includeTests bool) ([]string, error) {
	key := packageKey{Target: target, IncludeTests: includeTests}

	idx.mu.Lock()
	if entry, ok := idx.packages[key]; ok {
		files := append([]string(nil), entry.Files...)
		idx.mu.Unlock()
		return files, nil
	}
	idx.mu.Unlock()

	entry, err := loadPackageEntry(ctx, target, includeTests)
	if err != nil {
		return nil, err
	}

	idx.mu.Lock()
	idx.packages[key] = entry
	idx.mu.Unlock()
	return append([]string(nil), entry.Files...), nil
}

func (idx *sourceIndex) FileInfo(path string) (sourceFileInfo, error) {
	path = filepath.Clean(path)
	stat, err := os.Stat(path)
	if err != nil {
		return sourceFileInfo{}, fmt.Errorf("stat source file %s: %w", path, err)
	}

	idx.mu.Lock()
	if entry, ok := idx.files[path]; ok && entry.ModTime.Equal(stat.ModTime()) && entry.Size == stat.Size() {
		info := entry.Info
		idx.mu.Unlock()
		return info, nil
	}
	idx.mu.Unlock()

	info, err := parseSourceFile(path)
	if err != nil {
		return sourceFileInfo{}, err
	}

	idx.mu.Lock()
	idx.files[path] = fileEntry{ModTime: stat.ModTime(), Size: stat.Size(), Info: info}
	idx.mu.Unlock()
	return info, nil
}

func (idx *sourceIndex) PackageInfo(ctx context.Context, target string, includeTests bool) ([]sourceFileInfo, error) {
	files, err := idx.PackageFiles(ctx, target, includeTests)
	if err != nil {
		return nil, err
	}
	return idx.fileInfos(files)
}

func (idx *sourceIndex) ModuleFiles(ctx context.Context, target string, includeTests bool) ([]string, error) {
	entry, err := loadPackageEntry(ctx, target, includeTests)
	if err != nil {
		return nil, err
	}
	root, err := findModuleRoot(entry.Dir)
	if err != nil {
		return nil, err
	}

	idx.mu.Lock()
	if files, ok := idx.modules[moduleCacheKey(root, includeTests)]; ok {
		cached := append([]string(nil), files...)
		idx.mu.Unlock()
		return cached, nil
	}
	idx.mu.Unlock()

	files, err := walkModuleGoFiles(root, includeTests)
	if err != nil {
		return nil, err
	}

	idx.mu.Lock()
	idx.modules[moduleCacheKey(root, includeTests)] = append([]string(nil), files...)
	idx.mu.Unlock()
	return files, nil
}

func moduleCacheKey(root string, includeTests bool) string {
	if includeTests {
		return root + "|tests"
	}
	return root + "|notests"
}

func (idx *sourceIndex) fileInfos(files []string) ([]sourceFileInfo, error) {
	infos := make([]sourceFileInfo, len(files))
	var firstErr error
	var wg sync.WaitGroup
	var mu sync.Mutex
	for i, path := range files {
		wg.Add(1)
		go func(i int, path string) {
			defer wg.Done()
			info, err := idx.FileInfo(path)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				return
			}
			infos[i] = info
		}(i, path)
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	return infos, nil
}

func discoverCommentBreakpoints(ctx context.Context, _ *sourceIndex, target string, includeTests bool) ([]backend.BreakpointSpec, error) {
	return discoverCommentBreakpointsInPackage(ctx, target, includeTests)
}

func discoverCommentBreakpointsInModule(ctx context.Context, _ *sourceIndex, target string, includeTests bool) ([]backend.BreakpointSpec, error) {
	return discoverCommentBreakpointsAcrossModule(ctx, target, includeTests)
}

func dedupeBreakpointSpecs(specs []backend.BreakpointSpec) []backend.BreakpointSpec {
	seen := make(map[string]struct{}, len(specs))
	out := make([]backend.BreakpointSpec, 0, len(specs))
	for _, spec := range specs {
		if spec.Location == "" {
			continue
		}
		if _, ok := seen[spec.Location]; ok {
			continue
		}
		seen[spec.Location] = struct{}{}
		out = append(out, spec)
	}
	return out
}

type goListPackage struct {
	Dir          string
	GoFiles      []string
	TestGoFiles  []string
	XTestGoFiles []string
}

func loadPackageEntry(ctx context.Context, target string, includeTests bool) (packageEntry, error) {
	cmd := exec.CommandContext(ctx, "go", "list", "-json", target)
	output, err := cmd.Output()
	if err != nil {
		return packageEntry{}, fmt.Errorf("go list %s: %w", target, err)
	}

	var pkg goListPackage
	if err := json.Unmarshal(output, &pkg); err != nil {
		return packageEntry{}, fmt.Errorf("decode go list for %s: %w", target, err)
	}

	files := make([]string, 0, len(pkg.GoFiles)+len(pkg.TestGoFiles)+len(pkg.XTestGoFiles))
	for _, name := range pkg.GoFiles {
		files = append(files, filepath.Join(pkg.Dir, name))
	}
	if includeTests {
		for _, name := range pkg.TestGoFiles {
			files = append(files, filepath.Join(pkg.Dir, name))
		}
		for _, name := range pkg.XTestGoFiles {
			files = append(files, filepath.Join(pkg.Dir, name))
		}
	}

	return packageEntry{Dir: pkg.Dir, Files: files}, nil
}

func findModuleRoot(startDir string) (string, error) {
	dir := filepath.Clean(startDir)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("find module root for %s: go.mod not found", startDir)
		}
		dir = parent
	}
}

func walkModuleGoFiles(root string, includeTests bool) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if name == "vendor" || name == ".git" || strings.HasPrefix(name, ".") {
				if path == root {
					return nil
				}
				return filepath.SkipDir
			}
			if path != root {
				if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		if !includeTests && strings.HasSuffix(path, "_test.go") {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk module go files in %s: %w", root, err)
	}
	return files, nil
}

func parseSourceFile(path string) (sourceFileInfo, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return sourceFileInfo{}, fmt.Errorf("parse source file %s: %w", path, err)
	}

	lines, err := readSourceLines(path)
	if err != nil {
		return sourceFileInfo{}, err
	}

	info := sourceFileInfo{Path: path}
	if file.Name != nil {
		info.Package = file.Name.Name
	}
	info.Symbols = collectSymbols(fset, file, info.Package)
	info.Markers = collectBreakpointMarkers(fset, file, lines)
	return info, nil
}

func readSourceLines(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read source file %s: %w", path, err)
	}
	return strings.Split(string(data), "\n"), nil
}

func collectSymbols(fset *token.FileSet, file *ast.File, pkg string) []symbolInfo {
	var symbols []symbolInfo
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name == nil {
			continue
		}
		receiver := ""
		kind := symbolFunction
		qualified := pkg + "." + fn.Name.Name
		if fn.Recv != nil && len(fn.Recv.List) > 0 {
			receiver = types.ExprString(fn.Recv.List[0].Type)
			kind = symbolMethod
			qualified = receiver + "." + fn.Name.Name
		}
		symbols = append(symbols, symbolInfo{
			Name:      fn.Name.Name,
			Qualified: qualified,
			Kind:      kind,
			Receiver:  receiver,
			Line:      fset.Position(fn.Pos()).Line,
			IsTest:    strings.HasPrefix(fn.Name.Name, "Test"),
		})
	}
	return symbols
}

func collectBreakpointMarkers(fset *token.FileSet, file *ast.File, lines []string) []breakpointMarker {
	var markers []breakpointMarker
	for _, group := range file.Comments {
		for _, comment := range group.List {
			if !isBreakpointComment(comment.Text) {
				continue
			}
			line := fset.Position(comment.Pos()).Line
			endLine := fset.Position(comment.End()).Line
			markers = append(markers, breakpointMarker{
				Line:       line,
				TargetLine: resolveBreakpointTargetLine(lines, endLine),
				Raw:        comment.Text,
			})
		}
	}
	return markers
}

func isBreakpointComment(text string) bool {
	trimmed := strings.TrimSpace(text)
	if strings.HasPrefix(trimmed, "//") {
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "//"))
	}
	return trimmed == "breakpoint" || trimmed == "dlvpp:breakpoint"
}

func resolveBreakpointTargetLine(lines []string, commentEndLine int) int {
	for line := commentEndLine + 1; line <= len(lines); line++ {
		trimmed := strings.TrimSpace(lines[line-1])
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "/*") || strings.HasPrefix(trimmed, "*") {
			continue
		}
		return line
	}
	return 0
}
