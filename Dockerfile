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
RUN apk add --no-cache ca-certificates tzdata postgresql16-client
WORKDIR /app
COPY --from=builder /app/movies-api .
EXPOSE 8888
ENTRYPOINT ["./movies-api"]
