# Dockerfile (multi-stage build)
# syntax=docker/dockerfile:1

# Build stage
FROM golang:1.23-alpine AS builder
WORKDIR /app

# Copy module files first for layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy entire project structure (excluding unnecessary files)
COPY cmd/ ./cmd/
COPY internal/ ./internal/

# Build with proper package context
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/app ./cmd/worker/

# Runtime stage
FROM alpine:3.19
WORKDIR /
COPY --from=builder --chown=nonroot /bin/app /bin/app
ENV FLUENT_HOST=fluentbit \
    FLUENT_PORT=24224 \
    FLUENT_ASYNC=true \
    FLUENT_BUFFER_LIMIT=8192 \
    FLUENT_MAX_RETRY=13 \
    FLUENT_RETRY_WAIT=500
CMD ["/bin/app"]