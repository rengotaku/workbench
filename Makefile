.PHONY: install build run-todo run-note stop stop-todo stop-note status install-agent install-agent-todo install-agent-note lint test test-cov check ci clean help

# Default target
.DEFAULT_GOAL := help

# Variables
BIN_DIR := bin
COVERAGE_FILE := coverage.out
TODO_PORT ?= 8085
NOTE_PORT ?= 8086

# Auto-discover entry points under cmd/<name>/. Adding cmd/foo/main.go is
# sufficient — `make build` will produce bin/foo without further edits.
CMDS := $(notdir $(wildcard cmd/*))
BINARIES := $(addprefix $(BIN_DIR)/,$(CMDS))
.PHONY: $(BINARIES)

## install: Download dependencies
install:
	go mod download

## build: Build all cmd/* binaries (todo, todo-server, note, note-server)
build: $(BINARIES)

# Static pattern rule restricted to $(BINARIES) so it only matches discovered
# cmd/* entry points, not any other file that may live under bin/.
$(BINARIES): $(BIN_DIR)/%:
	@mkdir -p $(BIN_DIR)
	go build -o $@ ./cmd/$*

## run-todo: Run the todo server (:8085)
run-todo:
	go run ./cmd/todo-server

## run-note: Run the note server (:8086)
run-note:
	go run ./cmd/note-server

## stop: Stop both servers
stop: stop-todo stop-note

## stop-todo: Stop the todo server
stop-todo:
	@lsof -ti :$(TODO_PORT) -sTCP:LISTEN | xargs kill 2>/dev/null || true

## stop-note: Stop the note server
stop-note:
	@lsof -ti :$(NOTE_PORT) -sTCP:LISTEN | xargs kill 2>/dev/null || true

## status: Check if the servers are running
status:
	@lsof -ti :$(TODO_PORT) -sTCP:LISTEN >/dev/null 2>&1 && echo "todo-order: running (:$(TODO_PORT))" || echo "todo-order: stopped"
	@lsof -ti :$(NOTE_PORT) -sTCP:LISTEN >/dev/null 2>&1 && echo "note-stock: running (:$(NOTE_PORT))" || echo "note-stock: stopped"

## install-agent: Place the binaries and launchd plists for both (does not load them)
install-agent: install-agent-todo install-agent-note

## install-agent-todo: Build, place the todo binaries and the launchd plist (does not load it)
install-agent-todo:
	./scripts/install-todo-agent.sh --port $(TODO_PORT)

## install-agent-note: Build, place the note binaries and the launchd plist (does not load it)
install-agent-note:
	./scripts/install-note-agent.sh --port $(NOTE_PORT)

## lint: Run golangci-lint
lint:
	golangci-lint run

## test: Run tests
test:
	go test ./...

## test-cov: Run tests with coverage (internal/..., same scope as CI)
test-cov:
	go test -coverprofile=$(COVERAGE_FILE) ./internal/...
	go tool cover -func=$(COVERAGE_FILE)

## check: Run lint + test
check: lint test

## ci: Run lint + test with coverage
ci: lint test-cov

## clean: Remove build artifacts
clean:
	rm -rf $(BIN_DIR)/ $(COVERAGE_FILE)

## help: Show this help
help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## /  /'
