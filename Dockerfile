# Stage 1: Build explorer frontend (Vite SPA)
FROM node:22-alpine AS frontend
WORKDIR /frontend
ARG VITE_API_BASE=/v1/explorer
ARG VITE_BASE_REALTIME=/v1/base/realtime
ARG VITE_CHAIN_NAME="Liquid EVM"
ARG VITE_COIN=LQDTY
ARG VITE_CHAIN_ID=8675311
COPY explorer/app/package.json ./
RUN npm install --ignore-scripts
COPY explorer/app/ .
RUN npm run build

# Stage 2: Build Go binaries (indexer + explorer)
FROM golang:1.26-alpine AS builder
RUN apk add --no-cache gcc musl-dev sqlite-dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Embed frontend dist into Go binary
COPY --from=frontend /frontend/dist cmd/explorer/static/
ARG VERSION=dev
RUN CGO_ENABLED=1 CGO_CFLAGS="-D_LARGEFILE64_SOURCE" GOOS=linux go build \
    -ldflags="-s -w -X main.version=${VERSION}" -o /indexer ./cmd/indexer/
RUN CGO_ENABLED=1 CGO_CFLAGS="-D_LARGEFILE64_SOURCE" GOOS=linux go build \
    -ldflags="-s -w -X main.version=${VERSION}" -o /explorer ./cmd/explorer/

# Stage 3: Minimal runtime
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
ENTRYPOINT ["explorer"]
