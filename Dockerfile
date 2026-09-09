# luxfi/indexer — standalone chain-indexer daemon.
# The unified explorer + graph live in luxfi/explorer and luxfi/graph.
FROM golang:1.26.5-alpine AS builder
ENV GOTOOLCHAIN=auto
RUN apk add --no-cache gcc musl-dev sqlite-dev
WORKDIR /src
COPY . .
ARG VERSION=dev
# proxy.golang.org caches inconsistently for hanzoai/replicate@v0.6.0
# (different POPs serve different zip hashes). -mod=mod populates go.sum
# from whatever the proxy serves at build time and GOSUMDB=off skips
# sum.golang.org cross-checks.
RUN rm -f go.sum && CGO_ENABLED=1 CGO_CFLAGS="-D_LARGEFILE64_SOURCE" GOOS=linux \
    GOSUMDB=off go build -mod=mod \
    -ldflags="-s -w -linkmode external -extldflags '-static' -X main.version=${VERSION}" -o /indexerd ./cmd/indexerd/

# The binary and the TLS roots, nothing else. Statically linked, so no libc and
# no sqlite-libs to keep patched, and no shell for anything to run in.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /indexerd /usr/local/bin/indexerd
USER nonroot
VOLUME /data
ENV DATA_DIR=/data HTTP_ADDR=:8091
EXPOSE 8091
HEALTHCHECK --interval=30s --timeout=5s CMD wget -qO- http://localhost:8091/health || exit 1
ENTRYPOINT ["indexerd"]
