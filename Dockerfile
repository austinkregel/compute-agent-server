# Build with the repo root as context, e.g.:
#   docker build -f server/Dockerfile -t backup-server .
# (or `make -C server docker`, which does this via `docker build -f Dockerfile ..`)
#
# The repo root is required as build context because server/go.mod has a local
# `replace github.com/austinkregel/compute-agent => ../agent` — the go-build
# stage needs both agent/ and server/ present at that same relative layout.

# ---- Stage 1: build the dashboard SPA ----
FROM node:22-alpine AS client-build
WORKDIR /src/server/client
COPY server/client/package.json server/client/package-lock.json ./
RUN npm ci
COPY server/client/ ./
RUN npm run build

# ---- Stage 2: build the Go binary ----
FROM golang:1.25-bookworm AS go-build
WORKDIR /src
COPY agent/ ./agent/
COPY server/go.mod server/go.sum ./server/
WORKDIR /src/server
RUN go mod download
COPY server/ ./
RUN CGO_ENABLED=0 go build -o /out/backup-server ./cmd/server

# ---- Stage 3: runtime ----
FROM gcr.io/distroless/base-debian12 AS runtime
ENV TZ=UTC
WORKDIR /app
COPY --from=go-build /out/backup-server /app/backup-server
COPY --from=client-build /src/server/client/dist /app/client/dist
# server-config.json (SERVER_CONFIG_PATH), data/ (sqlite DSN default), and
# certs/ (optional TLS pair — falls back to plain HTTP if absent) are all
# resolved relative to CWD by the binary itself; mount them here as needed.
VOLUME ["/app/data", "/app/certs"]
EXPOSE 8443
ENTRYPOINT ["/app/backup-server"]
