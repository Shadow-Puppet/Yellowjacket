VERSION ?= dev
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
LDFLAGS := -X 'main.version=$(VERSION)' -X 'main.commit=$(COMMIT)'

# YJ_HOME isolates the dev build's config + database from a packaged
# install. Defaults to a sandbox under XDG data; override in .env to
# point elsewhere (or unset it there to share the real user dirs).
DEV_YJ_HOME ?= $(HOME)/.local/share/yellowjacket-dev

dev: setup generate clean
	if [ -f .env ]; then set -a; . ./.env; set +a; fi; : "$${YJ_HOME:=$(DEV_YJ_HOME)}"; export YJ_HOME; go tool wails dev -tags webkit2_41 -loglevel Debug -v 2

dev-debug: setup generate clean
	if [ -f .env ]; then set -a; . ./.env; set +a; fi; : "$${YJ_HOME:=$(DEV_YJ_HOME)}"; export YJ_HOME; YJ_LOG_LEVEL=debug go tool wails dev -tags webkit2_41 -loglevel Debug -v 2

# Base directory for fresh-install sandboxes. Deliberately NOT $TMPDIR:
# on most Linux distros /tmp is tmpfs (RAM-backed) and only a few GB, so
# the search index dump import — which wants 6GB free before it will even
# start, then streams multi-GB dumps through explore-staging/ — either
# fails its precheck or eats that much RAM. XDG cache is disk-backed
# everywhere and still throwaway.
FRESH_HOME_BASE ?= $(if $(XDG_CACHE_HOME),$(XDG_CACHE_HOME),$(HOME)/.cache)

# fresh-install runs dev against a brand-new YJ_HOME so every launch
# starts from a clean first-run state (no config.toml, no yj.db). The dir
# is not cleaned up automatically, so you can inspect it afterward; the
# printed path tells you where it is. Override the location with
# FRESH_HOME_BASE=/some/disk make fresh-install.
fresh-install: setup generate clean
	if [ -f .env ]; then set -a; . ./.env; set +a; fi; \
	mkdir -p "$(FRESH_HOME_BASE)"; \
	export YJ_HOME="$$(mktemp -d "$(FRESH_HOME_BASE)/yellowjacket-fresh.XXXXXX")"; \
	echo "==> fresh YJ_HOME=$$YJ_HOME"; \
	case "$$(findmnt -no FSTYPE -T "$$YJ_HOME" 2>/dev/null)" in \
	  tmpfs|ramfs) echo "==> WARNING: $$YJ_HOME is RAM-backed; the search index import needs ~6GB of real disk. Set FRESH_HOME_BASE to a disk-backed path." ;; \
	esac; \
	go tool wails dev -tags webkit2_41 -loglevel Debug -v 2

build-dev: generate
	go tool wails build -tags webkit2_41 -debug -clean -ldflags "$(LDFLAGS)"

build-prod: generate
	go tool wails build -tags webkit2_41 -clean -upx -ldflags "-s -w $(LDFLAGS)"

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
