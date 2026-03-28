# dlvpp

Minimal, opinionated Delve frontend for Go.

## Current commands

```bash
go run ./cmd/dlvpp version
go run ./cmd/dlvpp launch ./examples/hello
go run ./cmd/dlvpp launch --plain ./examples/hello
go run ./cmd/dlvpp launch ./path/to/your/package
go run ./cmd/dlvpp attach <pid>
go run ./cmd/dlvpp attach --plain <pid>
```

## Current behavior

`launch <package-or-path>` currently:
- starts a DAP-backed Delve session
- sets a default breakpoint at `main.main`
- continues to that breakpoint
- waits for interactive debugger commands until quit

### Output modes

- **Sticky mode (default)**
  - human-oriented output
  - re-renders a sliding source window centered on the current line after each stop
  - expands that source window to use the available terminal height in TTY mode
  - keeps the interactive command legend visible in the terminal UI
- **Plain mode (`--plain`, `-p`)**
  - compact, token-friendly output for agent/LLM-driven debugging
  - prints a small non-sticky source window around the current line
  - omits the runtime command legend to reduce noise; use `dlvpp help` for the command reference

### Interactive commands

- `n` — next
- `s` — step in
- `l` — show locals for the current frame
- `:b <location>` — create a breakpoint
- `q` — quit the session

## Example

```bash
go run ./cmd/dlvpp launch ./examples/hello
```

## TODO

### Done
- [x] Create the initial Go module and repo layout.
- [x] Define a transport-neutral backend interface in `internal/backend/backend.go`.
- [x] Implement a first DAP adapter.
- [x] Support session lifecycle: `Launch`, `Attach`, `Close`.
- [x] Add an opinionated default breakpoint at `main.main`.
- [x] Continue to the breakpoint and print the current stopped location.
- [x] Render a source window around the current line.
- [x] Add lightweight Go syntax highlighting with `go/scanner` and `go/token`.
- [x] Add build, test, lint, and fmt workflows via `Makefile`.
- [x] Split source rendering into `internal/sourceview/` and DAP wire types into `internal/backend/dap/types.go`.
- [x] Move session orchestration and snapshot building into `internal/session/` so future REPL/TUI frontends can share one controller.

### Next
- [ ] Implement stepping commands: `next`, `step in`, `step out`, `pause`.
- [ ] Add a small interactive REPL instead of waiting only for Enter.
- [ ] Re-render location and source context after each debugger action.
- [ ] Implement locals/args display below the source window.
- [ ] Implement expression evaluation (`p <expr>` style UX).
- [ ] Add breakpoint listing and clearing.
- [ ] Add goroutine inspection and selection.
- [ ] Decide whether to keep the synchronous wrapper model or move to a more event-driven client loop.
- [ ] Decide whether to add a second backend for Delve headless API or stay DAP-only.
- [ ] Reassess whether a richer TUI is needed later or if raw terminal output remains enough.

## Layout

- `cmd/dlvpp/main.go` — CLI entrypoint
- `internal/backend/backend.go` — transport-neutral debugger interface
- `internal/backend/dap/` — DAP adapter
- `internal/session/` — shared session controller and snapshot building for CLI/REPL/TUI frontends
- `internal/sourceview/` — source window rendering and Go syntax highlighting
- `examples/hello/` — sample target program

## Validation

```bash
make lint
make test
make build
go run ./cmd/dlvpp launch ./examples/hello
```

