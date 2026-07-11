# syntax=docker/dockerfile:1.6
ARG GO_VERSION=1.26


FROM node:20-alpine AS web
WORKDIR /web
COPY frontend/package.json frontend/package-lock.json* ./
RUN --mount=type=cache,target=/root/.npm npm install --no-audit --no-fund
COPY frontend/ ./
RUN npm run build


FROM golang:${GO_VERSION}-alpine AS builder
WORKDIR /src
COPY backend/go.mod backend/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY backend/ ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -ldflags='-s -w' -o /out/shop ./cmd/shop


FROM alpine:3.20 AS release
# hadolint ignore=DL3018
RUN apk add --no-cache ca-certificates && \
    addgroup -S app && adduser -S app -G app
COPY --from=builder /out/shop /app/shop
COPY --from=web /web/dist /app/web
RUN chown -R root:root /app && chmod 0755 /app/shop
USER app
WORKDIR /app
EXPOSE 8080
ENTRYPOINT ["/app/shop"]
