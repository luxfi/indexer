# Build stage
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /app

# Copy go mod files first for caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# Build with version info
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags "-X main.version=${VERSION}" -o /indexer ./cmd/indexer

# Runtime stage
FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY --from=builder /indexer /usr/local/bin/indexer

# Default environment
ENV RPC_ENDPOINT=""
ENV DATABASE_URL=""
ENV HTTP_PORT="4200"
ENV POLL_INTERVAL="30s"

EXPOSE 4200 4100 4300 4400 4500 4600 4700

ENTRYPOINT ["indexer"]
CMD ["--help"]
