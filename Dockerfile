# Multi-stage build for Go application with React web UI

# Stage 1: Build web application
FROM node:18-alpine AS web-builder

# Set working directory for web build
WORKDIR /web

# Copy web application files
COPY web/package*.json ./

# Install dependencies
RUN npm ci --only=production=false

# Copy web source code
COPY web/ ./

# Build the web application
RUN npm run build

# Stage 2: Build Go application
FROM golang:1.25.1-alpine AS go-builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Copy built web assets from web-builder stage
COPY --from=web-builder /web/dist ./web/dist

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o ems .

# Stage 3: Final runtime image
FROM alpine:latest

# Install runtime dependencies
RUN apk add --no-cache ca-certificates tzdata

ENV TZ=Europe/Riga

# Create non-root user
RUN addgroup -g 1001 ems && \
    adduser -D -u 1001 -G ems ems

# Set working directory
WORKDIR /app

# Copy binary from builder stage
COPY --from=go-builder /app/ems .

# Copy web assets from builder stage
COPY --from=go-builder /app/web/dist ./web/dist

# Copy configuration example
COPY config.json /app/config.json

# Create directories for logs and data
RUN mkdir -p /app/logs /app/data && \
    chown -R ems:ems /app

# Switch to non-root user
USER ems

# Expose application port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/api/health || exit 1

# Default command
ENTRYPOINT ["./ems"]
