# syntax=docker/dockerfile:1.6
# Unified Shop image: the Go backend also serves the built storefront SPA, so a
# Shop is a real, clickable site at "/" (plus /api, /metrics, /probe). Build
# context is the shop repo root.
ARG GO_VERSION=1.26

# --- storefront (Vite) build ---
FROM node:20-alpine AS web
WORKDIR /web
COPY frontend/package.json frontend/package-lock.json* ./
RUN --mount=type=cache,target=/root/.npm npm install --no-audit --no-fund
COPY frontend/ ./
RUN npm run build

# --- backend (Go) build ---
FROM golang:${GO_VERSION}-alpine AS builder
WORKDIR /src
COPY backend/go.mod backend/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY backend/ ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -ldflags='-s -w' -o /out/shop ./cmd/shop

# --- release ---
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
