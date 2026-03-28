package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"dlvpp/internal/backend"
)

func newLaunchRequest(target string) (backend.LaunchRequest, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return backend.LaunchRequest{}, errors.New("launch target is required")
	}

	if strings.HasSuffix(target, ".go") {
		if info, err := os.Stat(target); err == nil && !info.IsDir() {
			return backend.LaunchRequest{}, fmt.Errorf("launch expects a main package path or directory, not a .go file: %s\ntry: launch %s", target, filepath.Dir(target))
		}
	}

	return backend.LaunchRequest{
		Mode:   backend.LaunchModeDebug,
		Target: target,
	}, nil
}
