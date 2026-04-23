FROM --platform=linux/arm64 golang:1.22-alpine AS builder

WORKDIR /app

# Install git (needed by go mod download for some modules)
RUN apk add --no-cache git

# Copy go module files first for better layer caching
COPY go.mod go.sum* ./
RUN go mod download

# Copy source code
COPY . .

# Build all three binaries with static linking
RUN CGO_ENABLED=0 GOOS=linux GOARCH=arm64 \
    go build -ldflags="-s -w" -o /out/xdcc-dl ./cmd/xdcc-dl
RUN CGO_ENABLED=0 GOOS=linux GOARCH=arm64 \
    go build -ldflags="-s -w" -o /out/xdcc-search ./cmd/xdcc-search
RUN CGO_ENABLED=0 GOOS=linux GOARCH=arm64 \
    go build -ldflags="-s -w" -o /out/xdcc-browse ./cmd/xdcc-browse

# --- Runtime image ---
FROM --platform=linux/arm64 alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /out/xdcc-dl    /usr/local/bin/xdcc-dl
COPY --from=builder /out/xdcc-search /usr/local/bin/xdcc-search
COPY --from=builder /out/xdcc-browse /usr/local/bin/xdcc-browse

# Keep the container running so it can be exec'd into
CMD ["tail", "-f", "/dev/null"]
