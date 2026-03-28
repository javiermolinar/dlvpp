package main

import "dlvpp/internal/backend"

type breakpointLocation struct {
	File string
	Line int
}

func breakpointLocationFromBackend(bp *backend.Breakpoint) (breakpointLocation, bool) {
	if bp == nil || bp.Location.File == "" || bp.Location.Line <= 0 {
		return breakpointLocation{}, false
	}
	return breakpointLocation{File: bp.Location.File, Line: bp.Location.Line}, true
}

func rememberBreakpoint(state *viewState, bp *backend.Breakpoint) {
	if state == nil {
		return
	}
	location, ok := breakpointLocationFromBackend(bp)
	if !ok {
		return
	}
	for _, existing := range state.breakpoints {
		if existing == location {
			return
		}
	}
	state.breakpoints = append(state.breakpoints, location)
}

func initialBreakpointLocations(bps ...*backend.Breakpoint) []breakpointLocation {
	locations := make([]breakpointLocation, 0, len(bps))
	for _, bp := range bps {
		if location, ok := breakpointLocationFromBackend(bp); ok {
			locations = append(locations, location)
		}
	}
	return locations
}
