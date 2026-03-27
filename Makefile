BINARY := bin/dlvpp
GOFILES := $(shell find . -type f -name '*.go' -not -path './vendor/*')

.PHONY: help build test lint fmt all

help:
	@echo "Targets:"
	@echo "  make build  - build $(BINARY)"
	@echo "  make test   - run go test ./..."
	@echo "  make lint   - check gofmt and run golangci-lint via go tool"
	@echo "  make fmt    - format Go files with gofmt"
	@echo "  make all    - fmt check + lint + test + build"

build:
	@mkdir -p bin
	go build -o $(BINARY) ./cmd/dlvpp

test:
	go test ./...

lint:
	@test -n "$(GOFILES)"
	@test -z "$$(gofmt -l $(GOFILES))" || (echo "Go files need formatting. Run 'make fmt'." && gofmt -l $(GOFILES) && exit 1)
	go tool golangci-lint run ./...

fmt:
	@test -n "$(GOFILES)"
	gofmt -w $(GOFILES)

all: lint test build
