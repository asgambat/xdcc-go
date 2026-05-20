# ============================================================
# Stage 1 — Build frontend (Svelte + Vite)
# ============================================================
FROM node:22-alpine AS frontend-builder

WORKDIR /app/web

# Copy frontend source
COPY web/package.json web/package-lock.json* ./
RUN npm ci

COPY web/ .

# Build to web/dist/
RUN npm run build

# ============================================================
# Stage 2 — Build Go binaries
# ============================================================
FROM --platform=$BUILDPLATFORM golang:1.22-alpine AS builder

ARG TARGETOS
ARG TARGETARCH

WORKDIR /app

# Install git (needed by go mod download for some modules)
RUN apk add --no-cache git

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
# Stage 3 — Runtime image
# ============================================================
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

# Copy all binaries
COPY --from=builder /out/xdcc-dl      /usr/local/bin/xdcc-dl
COPY --from=builder /out/xdcc-search  /usr/local/bin/xdcc-search
COPY --from=builder /out/xdcc-browse  /usr/local/bin/xdcc-browse
COPY --from=builder /out/xdcc-server  /usr/local/bin/xdcc-server

# Copy default config
COPY --from=builder /app/config.yaml  /etc/xdcc-server/config.yaml

# Create data directory with download subdirectories
RUN mkdir -p /data/downloads/tmp /data/downloads/complete /data/logs

# Expose HTTP port (REST API + web UI)
EXPOSE 8080

# Persist database, downloads, logs
VOLUME ["/data"]

# Default config: use /data for all persistent files
ENV XDCC_HTTP_PORT=8080
ENV XDCC_DOWNLOAD_TEMP_DIR=/data/downloads/tmp
ENV XDCC_DOWNLOAD_DEST_DIR=/data/downloads/complete
ENV XDCC_LOGGING_FILE_PATH=/data/logs/xdcc-server.log

WORKDIR /data

# Default: start the server.
# Override with CLI tools: docker run xdcc-go xdcc-dl "/msg Bot xdcc send #5"
# Or pass server flags: use env vars (XDCC_HTTP_PORT, etc.) instead
CMD ["/usr/local/bin/xdcc-server", "--config", "/etc/xdcc-server/config.yaml"]
