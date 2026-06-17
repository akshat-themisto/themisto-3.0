FROM golang:1.23-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /gateway ./cmd/gateway

FROM alpine:3.19
RUN apk add --no-cache ca-certificates wget
COPY --from=builder /gateway /usr/local/bin/gateway
USER nobody:nobody
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -qO- http://localhost:8080/healthz || exit 1
ENTRYPOINT ["gateway"]
