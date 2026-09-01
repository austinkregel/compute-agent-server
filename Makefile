BINDIR  ?= dist
APP     ?= backup-server
GOFLAGS ?=

# ---------- Build ----------

.PHONY: build
build:
	@mkdir -p $(BINDIR)
	CGO_ENABLED=0 go build $(GOFLAGS) -o $(BINDIR)/$(APP) ./cmd/server

# Build for a specific OS/arch (e.g. make build-cross GOOS=linux GOARCH=arm64)
.PHONY: build-cross
build-cross:
	@mkdir -p $(BINDIR)
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GOFLAGS) -o $(BINDIR)/$(APP)-$(GOOS)-$(GOARCH) ./cmd/server

# ---------- Test ----------

.PHONY: test
test:
	go test ./...

.PHONY: test-race
test-race:
	go test -race ./...

.PHONY: test-v
test-v:
	go test -race -v ./...

# Dashboard (vitest) suite. Deliberately not folded into `test`: that target is
# the Go suite, and a Go-only change shouldn't require a Node toolchain to run
# it. Assumes dependencies are already installed (npm ci / npm install).
.PHONY: test-client
test-client:
	cd client && npm test

# Both planes this module ships — the Go control plane and the dashboard baked
# into its image. This is what CI runs before publishing.
.PHONY: test-all
test-all: test test-client

.PHONY: coverage
coverage:
	go test ./... -coverpkg=./... -coverprofile=coverage.out
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

# ---------- Lint / Vet ----------

.PHONY: vet
vet:
	go vet ./...

.PHONY: tidy
tidy:
	go mod tidy

# ---------- Run ----------

.PHONY: run
run: build
	./$(BINDIR)/$(APP) --config server-config.json

# Run directly without building a binary first
.PHONY: dev
dev:
	go run ./cmd/server --config server-config.json

# ---------- Client (Vue SPA) ----------

.PHONY: client
client:
	cd client && npm run build

# ---------- Docker ----------

.PHONY: docker
docker:
	docker build -t backup-server -f Dockerfile .

# ---------- Clean ----------

.PHONY: clean
clean:
	rm -rf $(BINDIR) coverage.out coverage.html
