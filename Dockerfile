FROM node:22-alpine AS frontend
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /internal/web/dist ./internal/web/dist
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o movies-api ./cmd/

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata postgresql16-client curl unzip

# xray-core — sidecar binary for VLESS/Reality proxy configs (internal/proxy/xray).
# Not needed at all if no proxy config ever uses type=vless, but cheap to always have.
ARG TARGETARCH
RUN set -eux; \
    case "$TARGETARCH" in \
      amd64) XRAY_ARCH="64" ;; \
      arm64) XRAY_ARCH="arm64-v8a" ;; \
      *) echo "unsupported arch for xray-core: $TARGETARCH" >&2; exit 1 ;; \
    esac; \
    curl -fsSL -o /tmp/xray.zip "https://github.com/XTLS/Xray-core/releases/latest/download/Xray-linux-${XRAY_ARCH}.zip" && \
    unzip -oj /tmp/xray.zip xray -d /usr/local/bin && \
    chmod +x /usr/local/bin/xray && \
    rm /tmp/xray.zip

WORKDIR /app
COPY --from=builder /app/movies-api .
ENV XRAY_BIN=/usr/local/bin/xray
EXPOSE 8888
ENTRYPOINT ["./movies-api"]
