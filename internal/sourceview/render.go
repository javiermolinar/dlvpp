package sourceview

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"os"
	"strings"
)

const (
	ansiReset   = "\x1b[0m"
	ansiDim     = "\x1b[2m"
	ansiYellow  = "\x1b[33m"
	ansiGreen   = "\x1b[32m"
	ansiBlue    = "\x1b[34m"
	ansiMagenta = "\x1b[35m"
	ansiGray    = "\x1b[90m"
)

func RenderWindow(path string, line int, contextLines int) (string, error) {
	renderedLines, err := renderedLines(path)
	if err != nil {
		return "", err
	}

	start := max(1, line-contextLines)
	end := min(len(renderedLines), line+contextLines)
	return formatRange(renderedLines, line, start, end)
}

func RenderRange(path string, line int, start int, end int) (string, error) {
	renderedLines, err := renderedLines(path)
	if err != nil {
		return "", err
	}

	if start <= 0 || end <= 0 {
		return "", errorsf("source range out of range: %d-%d", start, end)
	}
	if start > end {
		return "", errorsf("source range invalid: %d-%d", start, end)
	}

	start = max(1, start)
	end = min(len(renderedLines), end)
	return formatRange(renderedLines, line, start, end)
}

func RenderFunction(path string, line int) (string, error) {
	if !strings.HasSuffix(path, ".go") {
		return RenderWindow(path, line, 5)
	}

	start, end, err := enclosingFunctionRange(path, line)
	if err != nil {
		return "", err
	}
	return RenderRange(path, line, start, end)
}

func renderedLines(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read source: %w", err)
	}

	rendered := string(data)
	if strings.HasSuffix(path, ".go") {
		rendered = highlightGoSource(data)
	}

	lines := strings.Split(rendered, "\n")
	return lines, nil
}

func formatRange(lines []string, currentLine int, start int, end int) (string, error) {
	if currentLine <= 0 || currentLine > len(lines) {
		return "", errorsf("source line out of range: %d", currentLine)
	}
	if start <= 0 || start > len(lines) {
		return "", errorsf("source start line out of range: %d", start)
	}
	if end <= 0 || end > len(lines) {
		return "", errorsf("source end line out of range: %d", end)
	}
	if start > end {
		return "", errorsf("source range invalid: %d-%d", start, end)
	}

	var out strings.Builder
	out.WriteString("\n")
	for i := start; i <= end; i++ {
		text := lines[i-1]
		if i == currentLine {
			fmt.Fprintf(&out, "%s%s>%4d%s  %s\n", ansiYellow, ansiDim, i, ansiReset, text)
			continue
		}
		fmt.Fprintf(&out, "%s %4d%s  %s\n", ansiDim, i, ansiReset, text)
	}
	out.WriteString("\n")
	return out.String(), nil
}

func enclosingFunctionRange(path string, line int) (int, int, error) {
	if line <= 0 {
		return 0, 0, errorsf("source line out of range: %d", line)
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return 0, 0, fmt.Errorf("parse go source: %w", err)
	}

	bestStart := 0
	bestEnd := 0
	bestSize := 0

	ast.Inspect(file, func(node ast.Node) bool {
		if node == nil {
			return true
		}

		var startPos token.Pos
		var endPos token.Pos
		switch n := node.(type) {
		case *ast.FuncDecl:
			startPos = n.Pos()
			endPos = n.End()
		case *ast.FuncLit:
			startPos = n.Type.Func
			endPos = n.End()
		default:
			return true
		}

		start := fset.Position(startPos).Line
		end := fset.Position(endPos).Line
		if start == 0 || end == 0 || line < start || line > end {
			return true
		}

		size := end - start
		if bestStart == 0 || size < bestSize {
			bestStart = start
			bestEnd = end
			bestSize = size
		}
		return true
	})

	if bestStart == 0 {
		return 0, 0, errorsf("no enclosing function for line %d", line)
	}
	return bestStart, bestEnd, nil
}

func highlightGoSource(src []byte) string {
	var out bytes.Buffer

	fset := token.NewFileSet()
	file := fset.AddFile("snippet.go", -1, len(src))

	var s scanner.Scanner
	s.Init(file, src, nil, scanner.ScanComments)

	prev := 0
	for {
		pos, tok, lit := s.Scan()
		offset := file.Offset(pos)
		if offset > prev {
			out.Write(src[prev:offset])
		}
		if tok == token.EOF {
			break
		}

		text := lit
		if text == "" {
			text = tok.String()
		}
		if color := colorForToken(tok); color != "" {
			out.WriteString(color)
			out.WriteString(text)
			out.WriteString(ansiReset)
		} else {
			out.WriteString(text)
		}
		prev = offset + len(text)
	}

	if prev < len(src) {
		out.Write(src[prev:])
	}
	return out.String()
}

func colorForToken(tok token.Token) string {
	switch {
	case tok.IsKeyword():
		return ansiBlue
	case tok == token.STRING || tok == token.CHAR:
		return ansiGreen
	case tok == token.COMMENT:
		return ansiGray
	case tok == token.INT || tok == token.FLOAT || tok == token.IMAG:
		return ansiMagenta
	default:
		return ""
	}
}

func errorsf(format string, args ...any) error {
	return fmt.Errorf(format, args...)
}
