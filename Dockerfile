FROM golang:1.27-alpine AS builder
WORKDIR /src

RUN apk add --no-cache ca-certificates

COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/beszel-bridge ./cmd/beszel-bridge

FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

WORKDIR /app
COPY --from=builder --chown=10001:10001 /out/beszel-bridge /app/beszel-bridge
USER 10001:10001

EXPOSE 8123

ENTRYPOINT ["/app/beszel-bridge"]
