# Build
FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go install github.com/bufbuild/buf/cmd/buf@latest
RUN go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
RUN go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
RUN go install github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-grpc-gateway@latest
RUN go install github.com/google/gnostic/cmd/protoc-gen-openapi@latest
ENV PATH="/root/go/bin:${PATH}"
RUN buf generate
RUN CGO_ENABLED=0 GOOS=linux go build -o jwks-server ./cmd/jwks-server

# Run
FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
LABEL org.opencontainers.image.source="https://github.com/austindbirch/harbor_hook"
COPY --from=builder /app/jwks-server .
CMD ["./jwks-server"]
