# syntax=docker/dockerfile:1

FROM golang:1.26-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go tool templ generate && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

FROM alpine:3
RUN apk add --no-cache ca-certificates tzdata && \
    adduser -D -H -u 10001 app && \
    mkdir -p /app/data && chown app:app /app/data
WORKDIR /app
COPY --from=build /out/server /app/server

USER app
ENV ENVIRONMENT=production \
    HTTP_ADDR=:8000 \
    DATABASE_URL=/app/data/app.db
EXPOSE 8000
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s \
    CMD wget -qO- http://127.0.0.1:8000/healthz || exit 1
ENTRYPOINT ["/app/server"]
