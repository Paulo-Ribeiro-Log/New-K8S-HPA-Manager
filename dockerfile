# Build stage
FROM golang:1.21-alpine AS builder

# Install git and ca-certificates (needed for downloading dependencies)
RUN apk add --no-cache git ca-certificates tzdata

# Create appuser
RUN adduser -D -g '' appuser

# Set working directory
WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download
RUN go mod verify

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags='-w -s -extldflags "-static"' \
    -o new-k8s-hpa .

# Final stage
FROM scratch

# Import from builder
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /etc/passwd /etc/passwd

# Copy binary
COPY --from=builder /build/new-k8s-hpa /usr/local/bin/new-k8s-hpa

# Use appuser
USER appuser

# Set entrypoint
ENTRYPOINT ["/usr/local/bin/k8s-hpa-manager"]