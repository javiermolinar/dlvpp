package dap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"dlvpp/internal/backend"
)

type trackedBreakpoint struct {
	spec       backend.BreakpointSpec
	breakpoint backend.Breakpoint
}

func normalizeBreakpointSpec(spec backend.BreakpointSpec) (backend.BreakpointSpec, string, int, bool, error) {
	spec.Location = strings.TrimSpace(spec.Location)
	if spec.Location == "" {
		return backend.BreakpointSpec{}, "", 0, false, errors.New("breakpoint location is required")
	}

	file, line, ok := parseFileLineLocation(spec.Location)
	if !ok {
		return spec, "", 0, false, nil
	}
	if !filepath.IsAbs(file) {
		absFile, err := filepath.Abs(file)
		if err != nil {
			return backend.BreakpointSpec{}, "", 0, false, fmt.Errorf("resolve breakpoint path %q: %w", file, err)
		}
		file = absFile
	}
	spec.Location = fmt.Sprintf("%s:%d", file, line)
	return spec, file, line, true, nil
}

func trackedSpecs(items []trackedBreakpoint) []backend.BreakpointSpec {
	specs := make([]backend.BreakpointSpec, 0, len(items))
	for _, item := range items {
		specs = append(specs, item.spec)
	}
	return specs
}

func trackedBreakpointFromSpec(items []trackedBreakpoint, spec backend.BreakpointSpec) *backend.Breakpoint {
	for _, item := range items {
		if item.spec.Location == spec.Location && item.spec.Condition == spec.Condition {
			bp := item.breakpoint
			return &bp
		}
	}
	return nil
}

func buildSourceBreakpoints(specs []backend.BreakpointSpec) []map[string]any {
	breakpoints := make([]map[string]any, 0, len(specs))
	for _, spec := range specs {
		_, line, ok := parseFileLineLocation(spec.Location)
		if !ok {
			continue
		}
		entry := map[string]any{"line": line}
		if spec.Condition != "" {
			entry["condition"] = spec.Condition
		}
		breakpoints = append(breakpoints, entry)
	}
	return breakpoints
}

func buildFunctionBreakpoints(specs []backend.BreakpointSpec) []map[string]any {
	breakpoints := make([]map[string]any, 0, len(specs))
	for _, spec := range specs {
		entry := map[string]any{"name": spec.Location}
		if spec.Condition != "" {
			entry["condition"] = spec.Condition
		}
		breakpoints = append(breakpoints, entry)
	}
	return breakpoints
}

func decodeBreakpoints(body []byte, command string, want int) ([]dapBreakpoint, error) {
	var payload setBreakpointsBody
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode %s response: %w", command, err)
	}
	if len(payload.Breakpoints) != want {
		return nil, fmt.Errorf("decode %s response: expected %d breakpoints, got %d", command, want, len(payload.Breakpoints))
	}
	for _, bp := range payload.Breakpoints {
		if !bp.Verified {
			return nil, fmt.Errorf("breakpoint not verified: %s", bp.Message)
		}
	}
	return payload.Breakpoints, nil
}

func mapTrackedBreakpoints(specs []backend.BreakpointSpec, wires []dapBreakpoint) []trackedBreakpoint {
	items := make([]trackedBreakpoint, 0, len(specs))
	for i, spec := range specs {
		items = append(items, trackedBreakpoint{spec: spec, breakpoint: *mapBreakpoint(spec, wires[i])})
	}
	return items
}

func flattenTrackedBreakpoints(fileBreakpoints map[string][]trackedBreakpoint, functionBreakpoints []trackedBreakpoint) []backend.Breakpoint {
	out := make([]backend.Breakpoint, 0, len(functionBreakpoints))
	for _, group := range fileBreakpoints {
		for _, item := range group {
			out = append(out, item.breakpoint)
		}
	}
	for _, item := range functionBreakpoints {
		out = append(out, item.breakpoint)
	}
	sort.Slice(out, func(i, j int) bool {
		left := out[i]
		right := out[j]
		if left.Location.File != right.Location.File {
			return left.Location.File < right.Location.File
		}
		if left.Location.Line != right.Location.Line {
			return left.Location.Line < right.Location.Line
		}
		if left.Location.Function != right.Location.Function {
			return left.Location.Function < right.Location.Function
		}
		return left.ID < right.ID
	})
	return out
}

func (c *Client) CreateBreakpoint(ctx context.Context, spec backend.BreakpointSpec) (*backend.Breakpoint, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return nil, errors.New("dap client is not connected")
	}

	normalized, file, _, isFile, err := normalizeBreakpointSpec(spec)
	if err != nil {
		return nil, err
	}

	if isFile {
		group := c.fileBreakpoints[file]
		if existing := trackedBreakpointFromSpec(group, normalized); existing != nil {
			return existing, nil
		}
		specs := append(trackedSpecs(group), normalized)
		resp, err := c.requestLocked(ctx, "setBreakpoints", map[string]any{
			"source": map[string]any{
				"name": filepathBase(file),
				"path": file,
			},
			"breakpoints": buildSourceBreakpoints(specs),
		})
		if err != nil {
			return nil, err
		}
		wires, err := decodeBreakpoints(resp.Body, "setBreakpoints", len(specs))
		if err != nil {
			return nil, err
		}
		c.fileBreakpoints[file] = mapTrackedBreakpoints(specs, wires)
		return trackedBreakpointFromSpec(c.fileBreakpoints[file], normalized), nil
	}

	if existing := trackedBreakpointFromSpec(c.functionBreakpoints, normalized); existing != nil {
		return existing, nil
	}
	specs := append(trackedSpecs(c.functionBreakpoints), normalized)
	resp, err := c.requestLocked(ctx, "setFunctionBreakpoints", map[string]any{
		"breakpoints": buildFunctionBreakpoints(specs),
	})
	if err != nil {
		return nil, err
	}
	wires, err := decodeBreakpoints(resp.Body, "setFunctionBreakpoints", len(specs))
	if err != nil {
		return nil, err
	}
	c.functionBreakpoints = mapTrackedBreakpoints(specs, wires)
	return trackedBreakpointFromSpec(c.functionBreakpoints, normalized), nil
}

func (c *Client) Breakpoints(ctx context.Context) ([]backend.Breakpoint, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return nil, errors.New("dap client is not connected")
	}
	return flattenTrackedBreakpoints(c.fileBreakpoints, c.functionBreakpoints), nil
}

func (c *Client) ClearBreakpoint(ctx context.Context, id int) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return errors.New("dap client is not connected")
	}

	for file, group := range c.fileBreakpoints {
		idx := trackedBreakpointIndexByID(group, id)
		if idx < 0 {
			continue
		}
		next := append(append([]trackedBreakpoint(nil), group[:idx]...), group[idx+1:]...)
		if err := c.replaceFileBreakpointsLocked(ctx, file, next); err != nil {
			return err
		}
		return nil
	}

	idx := trackedBreakpointIndexByID(c.functionBreakpoints, id)
	if idx >= 0 {
		next := append(append([]trackedBreakpoint(nil), c.functionBreakpoints[:idx]...), c.functionBreakpoints[idx+1:]...)
		return c.replaceFunctionBreakpointsLocked(ctx, next)
	}

	return fmt.Errorf("breakpoint id not found: %d", id)
}

func trackedBreakpointIndexByID(items []trackedBreakpoint, id int) int {
	for i, item := range items {
		if item.breakpoint.ID == id {
			return i
		}
	}
	return -1
}

func (c *Client) replaceFileBreakpointsLocked(ctx context.Context, file string, next []trackedBreakpoint) error {
	specs := trackedSpecs(next)
	resp, err := c.requestLocked(ctx, "setBreakpoints", map[string]any{
		"source": map[string]any{
			"name": filepathBase(file),
			"path": file,
		},
		"breakpoints": buildSourceBreakpoints(specs),
	})
	if err != nil {
		return err
	}
	wires, err := decodeBreakpoints(resp.Body, "setBreakpoints", len(specs))
	if err != nil {
		return err
	}
	if len(specs) == 0 {
		delete(c.fileBreakpoints, file)
		return nil
	}
	c.fileBreakpoints[file] = mapTrackedBreakpoints(specs, wires)
	return nil
}

func (c *Client) replaceFunctionBreakpointsLocked(ctx context.Context, next []trackedBreakpoint) error {
	specs := trackedSpecs(next)
	resp, err := c.requestLocked(ctx, "setFunctionBreakpoints", map[string]any{
		"breakpoints": buildFunctionBreakpoints(specs),
	})
	if err != nil {
		return err
	}
	wires, err := decodeBreakpoints(resp.Body, "setFunctionBreakpoints", len(specs))
	if err != nil {
		return err
	}
	c.functionBreakpoints = mapTrackedBreakpoints(specs, wires)
	return nil
}
