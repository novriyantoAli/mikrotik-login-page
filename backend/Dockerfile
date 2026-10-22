# syntax=docker/dockerfile:1

# One image, two commands: `api` serves HTTP, `worker` runs background jobs.
# Both commands share the same binary set and configuration.
FROM golang:1.26-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
ARG COMMIT=unknown

RUN CGO_ENABLED=0 go build -trimpath \
        -ldflags "-s -w \
          -X github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/buildinfo.Version=${VERSION} \
          -X github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/buildinfo.Commit=${COMMIT}" \
        -o /out/api ./cmd/api \
    && CGO_ENABLED=0 go build -trimpath \
        -ldflags "-s -w \
          -X github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/buildinfo.Version=${VERSION} \
          -X github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/buildinfo.Commit=${COMMIT}" \
        -o /out/worker ./cmd/worker

FROM alpine:3.21 AS runner

RUN adduser -D -H -u 10001 app \
    && apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY --from=builder /out/api /out/worker /app/
COPY docker/entrypoint.sh /app/entrypoint.sh

RUN chmod +x /app/entrypoint.sh && chown -R app:app /app

USER app

# exec in the entrypoint keeps SIGTERM pointed at the Go process so both
# commands shut down gracefully.
ENTRYPOINT ["/app/entrypoint.sh"]
CMD ["api"]
