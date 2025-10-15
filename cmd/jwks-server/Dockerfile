# Build
FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/jwks-server/ ./cmd/jwks-server/
RUN CGO_ENABLED=0 GOOS=linux go build -o jwks-server ./cmd/jwks-server

# Run
FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
LABEL org.opencontainers.image.source="https://github.com/austindbirch/harbor_hook"
COPY --from=builder /app/jwks-server .
CMD ["./jwks-server"]
