FROM golang:1.24-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY cmd/nsq-monitor/ ./cmd/nsq-monitor/

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -o nsq-monitor ./cmd/nsq-monitor

# Final stage
FROM alpine:3.18
RUN apk add --no-cache ca-certificates wget
WORKDIR /app

COPY --from=builder /app/nsq-monitor .

EXPOSE 8084
CMD ["./nsq-monitor"]