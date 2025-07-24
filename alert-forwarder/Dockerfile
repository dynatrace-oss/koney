FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS builder

ARG TARGETOS
ARG TARGETARCH

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -ldflags="-w -s" -o alert-forwarder .

FROM gcr.io/distroless/static:nonroot

WORKDIR /app

COPY --from=builder /app/alert-forwarder .

# Expose port
EXPOSE 8000

# Set entrypoint
ENTRYPOINT ["./alert-forwarder"]
