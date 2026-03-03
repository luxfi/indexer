# Build stage - use BUILDPLATFORM to run Go natively on host (avoids QEMU segfaults)
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS builder
ARG TARGETOS TARGETARCH

RUN apk add --no-cache git ca-certificates

WORKDIR /app

# Copy go mod files first for caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# Build with version info - cross-compile via GOOS/GOARCH
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -tags postgres -ldflags "-X main.version=${VERSION}" -o /indexer ./cmd/indexer
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -tags postgres -ldflags "-X main.version=${VERSION}" -o /evmchains ./cmd/evmchains

# Runtime stage
FROM --platform=linux/amd64 alpine:3.19

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY --from=builder /indexer /usr/local/bin/indexer
COPY --from=builder /evmchains /usr/local/bin/evmchains
COPY configs/ /app/configs/

# Default environment
ENV RPC_ENDPOINT=""
ENV DATABASE_URL=""
ENV HTTP_PORT="4200"
ENV POLL_INTERVAL="30s"

EXPOSE 4000 4100 4200 4300 4400 4500 4600 4700 5000 5100 5200 5300 9000

ENTRYPOINT ["indexer"]
CMD ["--help"]
