package sourceview

import (
	"bytes"
	"fmt"
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
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read source: %w", err)
	}

	rendered := string(data)
	if strings.HasSuffix(path, ".go") {
		rendered = highlightGoSource(data)
	}

	lines := strings.Split(rendered, "\n")
	if line <= 0 || line > len(lines) {
		return "", fmt.Errorf("source line out of range: %d", line)
	}

	start := max(1, line-contextLines)
	end := min(len(lines), line+contextLines)

	var out strings.Builder
	out.WriteString("\n")
	for i := start; i <= end; i++ {
		text := lines[i-1]
		if i == line {
			fmt.Fprintf(&out, "%s%s>%4d%s  %s\n", ansiYellow, ansiDim, i, ansiReset, text)
			continue
		}
		fmt.Fprintf(&out, "%s %4d%s  %s\n", ansiDim, i, ansiReset, text)
	}
	out.WriteString("\n")
	return out.String(), nil
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
