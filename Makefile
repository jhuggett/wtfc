BINARY := wtfc
PKG    := ./...
BIN    := bin/$(BINARY)

.DEFAULT_GOAL := help

.PHONY: help build install run tui mcp inspect test vet fmt tidy check clean

help: ## list targets
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  \033[36m%-10s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## compile binary into bin/
	@mkdir -p bin
	go build -o $(BIN) .

install: ## go install into $$GOBIN / $$GOPATH/bin
	go install .

run: ## run with ARGS, e.g. make run ARGS="list --json"
	go run . $(ARGS)

tui: ## launch the TUI (no args)
	go run .

mcp: ## run the MCP stdio server (for piping into a host)
	go run . mcp

inspect: build ## open the MCP Inspector pointed at this build
	npx @modelcontextprotocol/inspector $(BIN) mcp

test: ## run tests
	go test $(PKG)

vet: ## go vet
	go vet $(PKG)

fmt: ## go fmt
	go fmt $(PKG)

tidy: ## go mod tidy
	go mod tidy

check: fmt vet test ## fmt + vet + test

clean: ## remove build artifacts
	rm -rf bin
