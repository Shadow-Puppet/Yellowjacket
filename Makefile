VERSION ?= dev
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
LDFLAGS := -X 'main.version=$(VERSION)' -X 'main.commit=$(COMMIT)'

dev: setup generate clean
	WEBKIT_DISABLE_DMABUF_RENDERER=1 go tool wails dev -tags webkit2_41 -loglevel Debug -v 2

build-dev: generate
	go tool wails build -tags webkit2_41 -debug -clean -ldflags "$(LDFLAGS)"

build-prod: generate
	go tool wails build -tags webkit2_41 -clean -obfuscated -upx -ldflags "-s -w $(LDFLAGS)"

build-frontend:
	cd frontend && pnpm install && pnpm build

clean:
	rm -rf frontend/dist
	rm -rf frontend/node_modules

generate:
	go generate ./...

lint:
	go tool golangci-lint run

test:
	go test -tags webkit2_41 -race -count=1 -timeout 120s ./...

vulncheck:
	go tool govulncheck ./...

install: ## Install all development dependencies (Go tools, frontend packages)
	go get -tool
	cd frontend && pnpm install

setup: install ## Install dependencies and set up git hooks
	go tool lefthook install

# Profiling (dev builds only — pprof server on :6060 starts automatically)
profile: ## Open interactive profiling menu (CPU, heap, trace, etc.)
	@./scripts/profile.sh

profile-cpu: ## Capture CPU profile and open flame graph in browser
	@./scripts/profile.sh cpu

profile-heap: ## Capture heap profile and open in browser
	@./scripts/profile.sh heap

profile-trace: ## Capture execution trace and open trace viewer
	@./scripts/profile.sh trace
