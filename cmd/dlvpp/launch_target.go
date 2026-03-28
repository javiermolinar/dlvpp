package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"dlvpp/internal/backend"
)

func newLaunchRequest(target string) (backend.LaunchRequest, error) {
	target, err := validatePackageTarget("launch", target)
	if err != nil {
		return backend.LaunchRequest{}, err
	}

	return backend.LaunchRequest{
		Mode:   backend.LaunchModeDebug,
		Target: target,
	}, nil
}

func newTestLaunchRequest(target string, selector string) (backend.LaunchRequest, error) {
	target, err := validatePackageTarget("test", target)
	if err != nil {
		return backend.LaunchRequest{}, err
	}

	req := backend.LaunchRequest{
		Mode:   backend.LaunchModeTest,
		Target: target,
	}
	if selector != "" {
		req.Args = testRunArgs(selector)
	}
	return req, nil
}

func validatePackageTarget(command string, target string) (string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", fmt.Errorf("%s target is required", command)
	}

	if strings.HasSuffix(target, ".go") {
		if info, err := os.Stat(target); err == nil && !info.IsDir() {
			return "", fmt.Errorf("%s expects a package path or directory, not a .go file: %s\ntry: %s %s", command, target, command, filepath.Dir(target))
		}
	}
	return target, nil
}

func testRunArgs(selector string) []string {
	parts := strings.Split(selector, "/")
	patternParts := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		patternParts = append(patternParts, "^"+regexp.QuoteMeta(part)+"$")
	}
	if len(patternParts) == 0 {
		return nil
	}
	return []string{"-test.run", strings.Join(patternParts, "/")}
}

func topLevelTestName(selector string) string {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return ""
	}
	part, _, _ := strings.Cut(selector, "/")
	return strings.TrimSpace(part)
}
