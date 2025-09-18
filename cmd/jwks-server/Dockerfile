FROM golang:1.24-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY cmd/jwks-server/ ./cmd/jwks-server/

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -o jwks-server ./cmd/jwks-server

FROM alpine:latest

RUN apk --no-cache add ca-certificates
WORKDIR /root/

COPY --from=builder /app/jwks-server .

CMD ["./jwks-server"]
