# Build stage
FROM golang:1.24-alpine AS builder

# Install dependencies
RUN apk add --no-cache git

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the CLI
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-w -s -X github.com/austindbirch/harbor_hook/cmd/harborctl/cmd.Version=1.0.0 -X github.com/austindbirch/harbor_hook/cmd/harborctl/cmd.GitCommit=$(git rev-parse --short HEAD) -X github.com/austindbirch/harbor_hook/cmd/harborctl/cmd.BuildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    -o harborctl \
    ./cmd/harborctl

# Final stage
FROM alpine:latest

# Install ca-certificates for HTTPS
RUN apk --no-cache add ca-certificates

WORKDIR /root/

# Copy the binary from builder stage
COPY --from=builder /app/harborctl .

# Make sure the binary is executable
RUN chmod +x harborctl

# Set the binary as the entry point
ENTRYPOINT ["./harborctl"]
