.PHONY: build build-indexer test test-coverage clean docker lint

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X main.version=$(VERSION)"

# Build targets
build:
	go build $(LDFLAGS) -o bin/explorer ./cmd/explorer

build-indexer:
	go build $(LDFLAGS) -o bin/indexer ./cmd/indexer

# Test targets
test:
	go test -v ./...

test-coverage:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

test-integration:
	go test -v -tags=integration ./test/...

# Docker — single binary with embedded frontend
docker:
	docker build -f Dockerfile.explorer -t ghcr.io/luxfi/explorer:$(VERSION) .
	docker tag ghcr.io/luxfi/explorer:$(VERSION) ghcr.io/luxfi/explorer:latest

# Code quality
lint:
	golangci-lint run ./...

fmt:
	go fmt ./...

vet:
	go vet ./...

# Development
dev:
	go run ./cmd/explorer --rpc=http://localhost:9630/ext/bc/C/rpc --chain-name="C-Chain" --coin=LUX --chain-id=96369

dev-config:
	go run ./cmd/explorer --config=chains.example.yaml

# Clean
clean:
	rm -rf bin/ coverage.out coverage.html

# Dependencies
deps:
	go mod download
	go mod tidy

# Database migrations (placeholder)
migrate-up:
	@echo "Run indexer with --init to create schema"

# Help
help:
	@echo "LUX Indexer - Go indexers for LUX Network native chains"
	@echo ""
	@echo "Build:"
	@echo "  make build          Build indexer binary"
	@echo "  make docker         Build Docker image"
	@echo ""
	@echo "Test:"
	@echo "  make test           Run unit tests"
	@echo "  make test-coverage  Run tests with coverage"
	@echo ""
	@echo "Development:"
	@echo "  make dev-xchain     Run X-Chain indexer locally"
	@echo "  make dev-pchain     Run P-Chain indexer locally"
	@echo "  make dev-zchain     Run Z-Chain indexer locally"
	@echo ""
	@echo "Code quality:"
	@echo "  make lint           Run linter"
	@echo "  make fmt            Format code"
