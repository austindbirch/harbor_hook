# Build
FROM golang:1.24 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/ingest ./cmd/ingest

# Run
FROM gcr.io/distroless/static:nonroot
COPY --from=build /out/ingest /ingest
USER nonroot:nonroot
ENTRYPOINT ["/ingest"]