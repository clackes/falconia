# syntax=docker/dockerfile:1.7
#
# The backend and the CLI come out of the same image.
#
# The instance distributes its own command line: whoever installs it downloads
# the binary from the server it will talk to, with that server's address
# already configured. It is the only way it works on a network without
# internet access, and it means "run this curl line" is all an agent needs.
FROM golang:1.25-alpine AS build
WORKDIR /src
RUN apk add --no-cache ca-certificates git

ARG VERSION=dev

COPY backend/go.mod backend/go.sum ./
RUN go mod download

COPY backend/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath \
      -ldflags="-s -w -X github.com/clackes/falconia/backend/internal/handlers.Version=${VERSION}" \
      -o /out/api ./cmd/api

# The CLI is a separate module with no external dependencies: build it for the
# combinations that cover practically every laptop.
WORKDIR /cli
COPY cli/ ./
RUN set -eu; \
    for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64; do \
      os="${target%/*}"; arch="${target#*/}"; \
      ext=""; [ "$os" = "windows" ] && ext=".exe"; \
      CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
        go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" \
        -o "/out/cli/falconia_${os}_${arch}${ext}" .; \
    done

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 10001 app
WORKDIR /app
COPY --from=build /out/api /app/api
COPY --from=build /out/cli /app/cli

# Blobs end up here when BLOB_BACKEND=fs. The folder belongs to the
# unprivileged user: the process never runs as root.
RUN mkdir -p /data/blobs && chown -R app:app /data
VOLUME ["/data"]

USER app
EXPOSE 8080 8081
HEALTHCHECK --interval=30s --timeout=3s --start-period=20s \
  CMD wget -qO- http://127.0.0.1:8080/api/health >/dev/null || exit 1
ENTRYPOINT ["/app/api"]
