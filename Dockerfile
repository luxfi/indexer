# luxfi/indexer — standalone chain-indexer daemon.
# The unified explorer + graph live in luxfi/explorer and luxfi/graph.
FROM golang:1.26-alpine AS builder
RUN apk add --no-cache gcc musl-dev sqlite-dev
WORKDIR /src
COPY go.mod go.sum ./
# proxy.golang.org has inconsistent caching for hanzoai/replicate@v0.6.0
# (different POPs serve different zip hashes). Regenerate go.sum from the
# proxy state we actually see at build time and skip sum.golang.org.
RUN rm -f go.sum && GOSUMDB=off go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=1 CGO_CFLAGS="-D_LARGEFILE64_SOURCE" GOOS=linux go build \
    -ldflags="-s -w -X main.version=${VERSION}" -o /indexerd ./cmd/indexerd/

FROM alpine:3.21
RUN apk add --no-cache ca-certificates sqlite-libs
COPY --from=builder /indexerd /usr/local/bin/indexerd
RUN adduser -D -u 65532 indexer
USER indexer
VOLUME /data
ENV DATA_DIR=/data HTTP_ADDR=:8091
EXPOSE 8091
HEALTHCHECK --interval=30s --timeout=5s CMD wget -qO- http://localhost:8091/health || exit 1
ENTRYPOINT ["indexerd"]
