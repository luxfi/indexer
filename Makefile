.PHONY: build build-all test test-coverage clean docker lint

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X main.version=$(VERSION)"

# Build targets
build:
	go build $(LDFLAGS) -o bin/indexer ./cmd/indexer

build-all: build
	@echo "Built lux-indexer $(VERSION)"

# Test targets
test:
	go test -v ./...

test-coverage:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

test-integration:
	go test -v -tags=integration ./test/...

# Docker targets
docker:
	docker build -t luxfi/indexer:$(VERSION) .
	docker tag luxfi/indexer:$(VERSION) luxfi/indexer:latest

docker-push: docker
	docker push luxfi/indexer:$(VERSION)
	docker push luxfi/indexer:latest

# Code quality
lint:
	golangci-lint run ./...

fmt:
	go fmt ./...

vet:
	go vet ./...

# Development
dev-xchain:
	go run ./cmd/indexer -chain xchain -rpc http://localhost:9630/ext/bc/X -db "postgres://blockscout:blockscout@localhost:5432/explorer_xchain" -port 4200

dev-pchain:
	go run ./cmd/indexer -chain pchain -rpc http://localhost:9630/ext/bc/P -db "postgres://blockscout:blockscout@localhost:5432/explorer_pchain" -port 4100

dev-zchain:
	go run ./cmd/indexer -chain zchain -rpc http://localhost:9630/ext/bc/Z -db "postgres://blockscout:blockscout@localhost:5432/explorer_zchain" -port 4400

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
