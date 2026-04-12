FROM golang:1.26-alpine AS builder
RUN apk add --no-cache gcc musl-dev sqlite-dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Build both binaries
ARG VERSION=dev
RUN mkdir -p cmd/explorer/static && echo '<html><body>Explorer API</body></html>' > cmd/explorer/static/index.html
RUN CGO_ENABLED=1 CGO_CFLAGS="-D_LARGEFILE64_SOURCE" GOOS=linux go build \
    -ldflags="-s -w -X main.version=${VERSION}" -o /indexer ./cmd/indexer/
RUN CGO_ENABLED=1 CGO_CFLAGS="-D_LARGEFILE64_SOURCE" GOOS=linux go build \
    -ldflags="-s -w -X main.version=${VERSION}" -o /explorer ./cmd/explorer/

FROM alpine:3.21
RUN apk add --no-cache ca-certificates sqlite-libs
COPY --from=builder /indexer /usr/local/bin/indexer
COPY --from=builder /explorer /usr/local/bin/explorer
RUN adduser -D -u 65532 explorer
USER explorer
VOLUME /data
ENV DATA_DIR=/data HTTP_ADDR=:8090
EXPOSE 8090
HEALTHCHECK --interval=30s --timeout=5s CMD wget -qO- http://localhost:8090/health || exit 1
# Default: multi-chain explorer. Override: indexer --chain cchain --rpc ...
ENTRYPOINT ["explorer"]
