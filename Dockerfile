# ============================================================
# Stage 1 — Build frontend (Svelte + Vite)
# ============================================================
FROM node:22-alpine AS frontend-builder

WORKDIR /app/web

# Copy frontend source
COPY web/package.json web/package-lock.json* ./
RUN npm ci

COPY web/ ./

# Build to web/dist/
RUN npm run build

# ============================================================
# Stage 2 — Build Go binaries
# ============================================================
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS builder

ARG TARGETOS
ARG TARGETARCH

WORKDIR /app

# Install git and ca-certificates (the latter is needed to copy certs to scratch)
RUN apk add --no-cache git ca-certificates

# Copy Go module files first for better layer caching
COPY go.mod go.sum* ./
RUN go mod download

# Copy source code
COPY . .

# Copy pre-built frontend from stage 1 (for go:embed)
COPY --from=frontend-builder /app/web/dist ./web/dist

# Build all binaries with static linking for the target platform
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -ldflags="-s -w" -o /out/xdcc-dl ./cmd/xdcc-dl && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -ldflags="-s -w" -o /out/xdcc-search ./cmd/xdcc-search && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -ldflags="-s -w" -o /out/xdcc-browse ./cmd/xdcc-browse && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -ldflags="-s -w" -o /out/xdcc-server ./cmd/xdcc-server

# ============================================================
# Stage 3 — Minimal runtime image (scratch)
# ============================================================
FROM scratch

# Copy CA certificates (needed for HTTPS requests to search APIs)
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy all binaries in a single layer
COPY --from=builder /out/ /usr/local/bin/

# Copy default config
COPY --from=builder /app/config.yaml /etc/xdcc-server/config.yaml

# Expose HTTP port (REST API + web UI)
EXPOSE 8080

# Persist database, downloads, logs
VOLUME ["/data"]

# Default config: use /data for all persistent files
ENV XDCC_HTTP_PORT=8080 \
    XDCC_DOWNLOAD_TEMP_DIR=/data/downloads/tmp \
    XDCC_DOWNLOAD_DEST_DIR=/data/downloads/complete \
    XDCC_LOGGING_FILE_PATH=/data/logs/xdcc-server.log

WORKDIR /data

CMD ["/usr/local/bin/xdcc-server", "--config", "/etc/xdcc-server/config.yaml"]
