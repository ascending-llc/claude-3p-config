.DEFAULT_GOAL := all

test:
	@go test -race ./...
.PHONY: test

coverage:
	@go test -race -coverprofile=coverage.out ./...
.PHONY: coverage

lint:
	@pre-commit run --all-files golangci-lint-full
.PHONY: lint

verify:
	@pre-commit run --all-files golangci-lint-config-verify
.PHONY: verify

fmt:
	@pre-commit run --all-files golangci-lint-fmt
.PHONY: fmt

lint-all:
	@pre-commit run --all-files
.PHONY: lint-all

# Scan the staged changes for secrets, as the pre-commit hook does.
gelato:
	@pre-commit run gelato
.PHONY: gelato

# Render config/config.json into dist/macos and dist/windows for the products in
# PRODUCTS, e.g. make policies PRODUCTS="claude codex".
policies:
	@go run ./cmd/ccwb-policy $(PRODUCTS)
.PHONY: policies

# Build the Codex setup program for Windows with config/config.json built in.
codex-setup:
	@test -f config/config.json || { echo "config/config.json is missing: copy config/config.example.json and fill it in" >&2; exit 1; }
	@GOOS=windows GOARCH=amd64 go build -trimpath -o dist/ccwb-codex-setup.exe ./cmd/ccwb-codex-setup
.PHONY: codex-setup

# Re-render the committed examples/ from config/config.example.json.
examples:
	@go run ./cmd/ccwb-policy -example
.PHONY: examples

all: test lint
.PHONY: all
