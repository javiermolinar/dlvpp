package parser

import (
	"fmt"
	"strconv"
	"strings"
)

func ParseInt(input string) (int, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return 0, fmt.Errorf("empty input")
	}
	value, err := strconv.Atoi(trimmed)
	if err != nil {
		return 0, fmt.Errorf("parse int %q: %w", input, err)
	}
	return value, nil
}
