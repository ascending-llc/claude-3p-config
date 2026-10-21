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

# Render config/config.json into dist/macos and dist/windows.
policies:
	@go run ./cmd/ccwb-policy
.PHONY: policies

# Re-render the committed examples/ from config/config.example.json.
examples:
	@go run ./cmd/ccwb-policy -example
.PHONY: examples

all: test lint
.PHONY: all
