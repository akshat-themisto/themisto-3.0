FROM golang:1.24-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /backend ./cmd/backend

FROM alpine:3.19
RUN apk add --no-cache ca-certificates wget
COPY --from=builder /backend /usr/local/bin/backend
USER nobody:nobody
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -qO- http://localhost:8443/healthz || exit 1
ENTRYPOINT ["backend"]
