# =============================================================================
# HuggingFace Model Downloader - Docker Image
# =============================================================================
# Multi-stage build for minimal image size
#
# Build:
#   docker build -t hfdownloader .
#
# Run CLI:
#   docker run --rm -v ./models:/data hfdownloader download TheBloke/Mistral-7B-GGUF -o /data
#
# Run Web Server:
#   docker run --rm -p 8080:8080 -v ./models:/data hfdownloader serve --port 8080 --models-dir /data/Models --datasets-dir /data/Datasets
#
# With HuggingFace token:
#   docker run --rm -e HF_TOKEN=hf_xxx -p 8080:8080 hfdownloader serve
#
# Credits: Original Docker support suggested by cdeving (#50)
# =============================================================================

# Build stage
FROM golang:1.23-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates

WORKDIR /src

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /hfdownloader ./cmd/hfdownloader

# =============================================================================
# Final stage - minimal image
# =============================================================================
FROM alpine:3.19

# Install ca-certificates for HTTPS
RUN apk add --no-cache ca-certificates tzdata

# Create non-root user
RUN adduser -D -u 1000 hfdownloader

# Copy binary from builder
COPY --from=builder /hfdownloader /usr/local/bin/hfdownloader

# Create data directory
RUN mkdir -p /data/Models /data/Datasets && chown -R hfdownloader:hfdownloader /data

# Switch to non-root user
USER hfdownloader

WORKDIR /data

# Default to showing help
ENTRYPOINT ["/usr/local/bin/hfdownloader"]
CMD ["--help"]

# Expose web server port
EXPOSE 8080

# Labels
LABEL org.opencontainers.image.source="https://github.com/bodaay/HuggingFaceModelDownloader"
LABEL org.opencontainers.image.description="Fast, concurrent CLI and Web UI for downloading models and datasets from HuggingFace"
LABEL org.opencontainers.image.licenses="Apache-2.0"

