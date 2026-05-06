.PHONY: build test test-coverage clean docker lint fmt vet dev deps

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X main.version=$(VERSION)"

build:
	go build $(LDFLAGS) -o bin/indexerd ./cmd/indexerd

test:
	go test -v ./...

test-coverage:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

docker:
	docker build -t ghcr.io/luxfi/indexer:$(VERSION) .
	docker tag ghcr.io/luxfi/indexer:$(VERSION) ghcr.io/luxfi/indexer:latest

lint:
	golangci-lint run ./...

fmt:
	go fmt ./...

vet:
	go vet ./...

dev:
	go run ./cmd/indexerd --rpc=http://localhost:9630/ext/bc/C/rpc --chain-name="C-Chain" --coin=LUX --chain-id=96369

dev-config:
	go run ./cmd/indexerd --config=chains.example.yaml

clean:
	rm -rf bin/ coverage.out coverage.html

deps:
	go mod download
	go mod tidy
