# Build
FROM golang:1.24-alpine AS build
WORKDIR /src
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
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/fake-receiver ./cmd/fake-receiver

# Run
FROM gcr.io/distroless/static:nonroot
COPY --from=build /out/fake-receiver /fake-receiver
LABEL org.opencontainers.image.source="https://github.com/austindbirch/harbor_hook"
USER nonroot:nonroot
ENTRYPOINT ["/fake-receiver"]