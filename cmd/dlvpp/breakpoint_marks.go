package main

import "dlvpp/internal/backend"

type breakpointRecord struct {
	ID       int
	File     string
	Line     int
	Function string
}

func breakpointRecordFromBackend(bp *backend.Breakpoint) (breakpointRecord, bool) {
	if bp == nil {
		return breakpointRecord{}, false
	}
	if bp.Location.File == "" || bp.Location.Line <= 0 {
		return breakpointRecord{}, false
	}
	return breakpointRecord{
		ID:       bp.ID,
		File:     bp.Location.File,
		Line:     bp.Location.Line,
		Function: bp.Location.Function,
	}, true
}

func rememberBreakpoint(state *viewState, bp *backend.Breakpoint) {
	if state == nil {
		return
	}
	record, ok := breakpointRecordFromBackend(bp)
	if !ok {
		return
	}
	for i, existing := range state.breakpoints {
		if existing.File == record.File && existing.Line == record.Line {
			state.breakpoints[i] = record
			return
		}
	}
	state.breakpoints = append(state.breakpoints, record)
}
