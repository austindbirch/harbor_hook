# Build
FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o nsq-monitor ./cmd/nsq-monitor

# Run
FROM alpine:3.18
RUN apk add --no-cache ca-certificates wget
WORKDIR /app
LABEL org.opencontainers.image.source="https://github.com/austindbirch/harbor_hook"
COPY --from=builder /app/nsq-monitor .
EXPOSE 8084
CMD ["./nsq-monitor"]