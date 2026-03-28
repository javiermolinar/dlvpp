# dlvpp

Minimal, opinionated Delve frontend for Go.

`dlvpp` is a small DAP-backed CLI that launches or attaches to Delve, stops at a sensible default breakpoint, and keeps the debugger loop intentionally compact.

## What works today

- launch a Go package/path with a default breakpoint at `main.main`
- attach to an existing process by PID
- continue execution, step with `next`, and step in
- inspect locals for the current frame
- inspect captured program output (`stdout`/`stderr`) during or after execution
- create breakpoints interactively with `:b <location>`
- use either a sticky TTY-oriented view or a compact plain mode for agents/LLMs
- render a sliding source window around the current line
- expand sticky mode to the available terminal height in TTY sessions
- validate launch targets and reject obvious `main.go` file mistakes

## Requirements

- Go
- [`dlv`](https://github.com/go-delve/delve) available on `PATH`

## Build and run

```bash
make build
./bin/dlvpp help
```

You can also run it directly with `go run`:

```bash
go run ./cmd/dlvpp help
go run ./cmd/dlvpp launch ./examples/hello
go run ./cmd/dlvpp launch --plain ./examples/hello
go run ./cmd/dlvpp attach <pid>
go run ./cmd/dlvpp attach --plain <pid>
```

## Usage

```text
dlvpp version
dlvpp launch [-p|--plain] <package-or-path>
dlvpp attach [-p|--plain] <pid>
```

### Modes

#### Sticky mode (default)

- human-oriented terminal view
- re-renders the current stopped location after each debugger action
- shows a sliding source window centered on the current line
- expands that source window to terminal height when output is a TTY
- keeps the command legend visible in TTY sessions
- colorizes locals and output inspections in TTY mode
- lets `Esc` return from locals/output inspection back to the source view

#### Plain mode (`--plain`, `-p`)

- compact, token-friendly output for scripts, agents, and LLMs
- prints a small non-sticky source window around the current line
- omits the repeated command legend to reduce noise
- keeps output easy to parse with stable prefixes like `stop`, `exit`, `stdout |`, and `stderr |`
- prints a delimited output block on exit when captured program output exists

## Interactive commands

- `c` — continue
- `n` — next
- `s` — step in
- `l` — show locals for the current frame
- `o` — show captured program output
- `:b <location>` — create a breakpoint
- `q` — quit the session

Use `Esc` in sticky TTY mode to leave the current inspection view.

## Example

```bash
go run ./cmd/dlvpp launch --plain ./examples/hello
```

Example plain output:

```text
stop main.main examples/hello/main.go:10
   9 | 
> 10 | func main() {
  11 | 	message := "hello delve world"
```

After stepping through program exit, plain mode can emit a captured output block like:

```text
OUTPUT-BEGIN
stdout | hello delve world
stdout | total: 42
OUTPUT-END
```

## Project layout

- `cmd/dlvpp/` — CLI entrypoint, command loop, and terminal/plain views
- `internal/backend/backend.go` — transport-neutral debugger interface
- `internal/backend/dap/` — Delve DAP adapter and wire types
- `internal/session/` — shared session controller and snapshot building
- `internal/sourceview/` — source window rendering and Go syntax highlighting
- `examples/hello/` — sample target program

## Status

### Implemented

- DAP-backed backend abstraction
- launch, attach, and close lifecycle
- default bootstrap breakpoint at `main.main`
- source snapshots and source window rendering
- sticky terminal rendering with height-aware source windows
- compact plain output mode
- interactive command loop
- `continue`, `next`, and `step in`
- locals inspection
- captured output inspection
- interactive breakpoint creation
- build/test/lint/fmt workflow via `Makefile`

### Next likely improvements

- `step out` and `pause` commands
- first-class test debugging, including launching a package in test mode and targeting a specific test or subtest by name
- expression evaluation (`p <expr>` style UX), with variable mutation or assignment as a later follow-up once session/state tracking is mature enough
- breakpoint listing and clearing
- code-comment breakpoints such as `//breakpoint` or `// dlvpp:breakpoint` that resolve into file/line breakpoints on launch
- first-class breakpoint management, with `b` to list active breakpoints and `:b ...` to create, search, and manage them interactively
- scoped breakpoint search, for example filtering files first and then resolving matching function symbols from that subset (ripgrep-style targets like `@modules/hola:New`)
- goroutine listing and navigation, including selecting a goroutine to inspect its stack and current source location
- optional pprof/profile capture and inspection during debug sessions, with profile artifacts or summaries attached to session exports
- session export for postmortem and LLM-assisted analysis (for example, a structured HTML timeline with stops, source, output, and locals changes)
- session handover between human and LLM operators, including saved debugger context, detach/reattach workflows, and state restoration in a new `dlvpp` instance
- deciding whether a richer TUI is worth adding later

## Validation

```bash
make lint
make test
make build
go run ./cmd/dlvpp help
go run ./cmd/dlvpp launch ./examples/hello
```
