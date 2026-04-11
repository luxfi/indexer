# Single-image explorer: Go binary + embedded Next.js static export
#
# Build:
#   docker build -f Dockerfile.explorer -t ghcr.io/luxfi/explorer .
#
# Run:
#   docker run -p 8090:8090 -v explorer-data:/data \
#     -e RPC_ENDPOINT=http://node:9650/ext/bc/C/rpc \
#     ghcr.io/luxfi/explorer
#
# Multi-chain:
#   docker run -p 8090:8090 -v explorer-data:/data \
#     -v ./chains.yaml:/etc/explorer/chains.yaml \
#     ghcr.io/luxfi/explorer --config=/etc/explorer/chains.yaml

ARG GO_VERSION=1.26
ARG NODE_VERSION=22
ARG FRONTEND_REPO=https://github.com/luxfi/explore.git
ARG FRONTEND_REF=main

# ---- Stage 1: Build frontend ----
FROM node:${NODE_VERSION}-alpine AS frontend
ARG FRONTEND_REPO
ARG FRONTEND_REF

RUN apk add --no-cache git
WORKDIR /app
RUN git clone --depth=1 --branch=${FRONTEND_REF} ${FRONTEND_REPO} .
RUN corepack enable && pnpm install --frozen-lockfile

# Force static export mode
RUN sed -i "s/output: 'standalone'/output: 'export'/" next.config.js

# Build with default env (override at runtime via NEXT_PUBLIC_* at deploy)
ENV NEXT_PUBLIC_API_BASE_PATH=/v1/explorer
RUN pnpm build

# ---- Stage 2: Build Go binary ----
FROM golang:${GO_VERSION}-alpine AS builder

RUN apk add --no-cache gcc musl-dev

ARG VERSION=dev
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Embed frontend static export
COPY --from=frontend /app/out /src/cmd/explorer/static

RUN CGO_ENABLED=1 GOOS=linux go build \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /explorer ./cmd/explorer/

# ---- Stage 3: Runtime ----
FROM alpine:3.21

RUN apk add --no-cache ca-certificates

COPY --from=builder /explorer /usr/local/bin/explorer

RUN adduser -D -u 65532 explorer
USER explorer

VOLUME /data
ENV DATA_DIR=/data
ENV HTTP_ADDR=:8090

EXPOSE 8090

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s \
  CMD wget -qO- http://localhost:8090/health || exit 1

ENTRYPOINT ["explorer"]
