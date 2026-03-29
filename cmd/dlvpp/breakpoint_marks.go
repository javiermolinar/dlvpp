package main

import (
	"dlvpp/internal/backend"
	"dlvpp/internal/sourceview"
)

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
	function := bp.Location.Function
	if function == "" {
		if resolved, err := sourceview.EnclosingFunctionName(bp.Location.File, bp.Location.Line); err == nil {
			function = resolved
		}
	}
	return breakpointRecord{
		ID:       bp.ID,
		File:     bp.Location.File,
		Line:     bp.Location.Line,
		Function: function,
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

func breakpointRecordsFromBackend(bps []backend.Breakpoint) []breakpointRecord {
	records := make([]breakpointRecord, 0, len(bps))
	for _, bp := range bps {
		bp := bp
		if record, ok := breakpointRecordFromBackend(&bp); ok {
			records = append(records, record)
		}
	}
	return records
}
