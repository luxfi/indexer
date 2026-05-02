# luxfi/indexer — Go backend only (API + block indexer)
FROM golang:1.26-alpine AS builder
RUN apk add --no-cache gcc musl-dev sqlite-dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
# Ensure embedded static/ is non-empty (real Vite build copied in via COPY above).
# If the build context did not include the SPA, fall back to a minimal placeholder
# so go:embed succeeds. Real builds ship the full SPA from cmd/explorer/static/.
RUN test -s cmd/explorer/static/index.html || ( \
        mkdir -p cmd/explorer/static && \
        echo '<html><body>Explorer placeholder — SPA not built. Run vite build before docker build.</body></html>' > cmd/explorer/static/index.html \
    )
RUN CGO_ENABLED=1 CGO_CFLAGS="-D_LARGEFILE64_SOURCE" GOOS=linux go build \
    -ldflags="-s -w -X main.version=${VERSION}" -o /explorer ./cmd/explorer/

FROM alpine:3.21
RUN apk add --no-cache ca-certificates sqlite-libs
COPY --from=builder /explorer /usr/local/bin/explorer
RUN adduser -D -u 65532 explorer
USER explorer
VOLUME /data
ENV DATA_DIR=/data HTTP_ADDR=:8090
EXPOSE 8090
HEALTHCHECK --interval=30s --timeout=5s CMD wget -qO- http://localhost:8090/health || exit 1
ENTRYPOINT ["explorer"]
