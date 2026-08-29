# Build with server/ itself as context, e.g.:
#   docker build -t backup-server .        (run from within server/)
#   docker build -f server/Dockerfile -t backup-server server/   (run from repo root)
#
# server/ depends on github.com/austinkregel/compute-agent as a real published
# module (see go.mod) rather than a local checkout, so this build needs
# nothing from outside server/ itself.

# ---- Stage 1: build the dashboard SPA ----
FROM node:22-alpine AS client-build
WORKDIR /src/client
COPY client/package.json client/package-lock.json ./
RUN npm ci
COPY client/ ./
RUN npm run build

# ---- Stage 2: build the Go binary ----
FROM golang:1.25-bookworm AS go-build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/backup-server ./cmd/server

# ---- Stage 3: runtime ----
FROM gcr.io/distroless/base-debian12 AS runtime
ENV TZ=UTC
WORKDIR /app
COPY --from=go-build /out/backup-server /app/backup-server
COPY --from=client-build /src/client/dist /app/client/dist
# server-config.json (SERVER_CONFIG_PATH), data/ (sqlite DSN default), and
# certs/ (optional TLS pair — falls back to plain HTTP if absent) are all
# resolved relative to CWD by the binary itself; mount them here as needed.
VOLUME ["/app/data", "/app/certs"]
EXPOSE 8443
# Re-invokes this same binary (--healthcheck) rather than shelling out — the
# distroless base has no shell/curl. The probe hits /healthz. start-period
# covers OIDC discovery, which retries with backoff for up to 5 minutes before
# the listener binds (internal/server.New).
HEALTHCHECK --interval=30s --timeout=5s --start-period=330s --retries=3 \
  CMD ["/app/backup-server", "--healthcheck"]
ENTRYPOINT ["/app/backup-server"]
