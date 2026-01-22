ARG BUILDPLATFORM
FROM --platform=${BUILDPLATFORM:-linux/amd64} golang:1.24-alpine AS builder

# These ARGs are automatically populated by Docker Buildx
ARG TARGETOS
ARG TARGETARCH

WORKDIR /app

# Cache dependencies separately
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build statically linked binary
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -ldflags="-s -w" -o modbus-cli cmd/modbus-cli/*.go

FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /
COPY --from=builder /app/modbus-cli /modbus-cli

USER nonroot:nonroot

ENTRYPOINT ["/modbus-cli"]

