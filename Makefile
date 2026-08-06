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

# Named, persistent sandboxes: `make sandbox foo` runs dev against
# $(FRESH_HOME_BASE)/yellowjacket-sandbox-foo, creating it on first use
# and reusing it (never deleting) afterward, so you can keep several
# long-lived states around — one with an imported search index, one with
# a small library, etc. `make sandbox-foo` is the same thing.
#
# `make sandboxes` lists the ones that exist.
#
# `make sandbox-rm foo [bar ...]` deletes them again, after confirming.
#
# The bare words after `sandbox` / `sandbox-rm` are extra make goals, so
# they need do-nothing rules to keep make from complaining. Those rules
# exist only when one of those is the first goal, so typos in other
# targets still fail loudly.
SANDBOX_DIR = $(FRESH_HOME_BASE)/yellowjacket-sandbox
ifneq (,$(filter $(firstword $(MAKECMDGOALS)),sandbox sandbox-rm))
SANDBOX_ARGS := $(wordlist 2,$(words $(MAKECMDGOALS)),$(MAKECMDGOALS))
SANDBOX_NAME := $(firstword $(SANDBOX_ARGS))
$(foreach a,$(SANDBOX_ARGS),$(eval $(a):;@:))
endif

sandbox: ## Run dev against a named, persistent YJ_HOME: make sandbox <name>
	@if [ -z "$(SANDBOX_NAME)" ]; then \
	  echo "usage: make sandbox <name>   (e.g. make sandbox foo)" >&2; exit 2; \
	fi
	@$(MAKE) --no-print-directory sandbox-$(SANDBOX_NAME)

sandbox-rm: ## Delete named sandboxes: make sandbox-rm <name> [name ...]
	@if [ -z "$(SANDBOX_ARGS)" ]; then \
	  echo "usage: make sandbox-rm <name> [name ...]" >&2; exit 2; \
	fi
	@set -e; \
	targets=""; \
	for n in $(SANDBOX_ARGS); do \
	  d="$(SANDBOX_DIR)-$$n"; \
	  if [ -d "$$d" ]; then \
	    echo "  $$(du -sh "$$d" 2>/dev/null | cut -f1)	$$d"; \
	    targets="$$targets $$d"; \
	  else \
	    echo "  (no such sandbox: $$n)" >&2; \
	  fi; \
	done; \
	if [ -z "$$targets" ]; then exit 1; fi; \
	if [ "$(FORCE)" != "1" ]; then \
	  printf "delete the above? [y/N] "; read -r ans; \
	  case "$$ans" in y|Y|yes|YES) ;; *) echo "aborted"; exit 1 ;; esac; \
	fi; \
	rm -rf $$targets; \
	echo "==> removed"

sandbox-%: setup generate clean
	if [ -f .env ]; then set -a; . ./.env; set +a; fi; \
	export YJ_HOME="$(SANDBOX_DIR)-$*"; \
	mkdir -p "$$YJ_HOME"; \
	echo "==> sandbox '$*' YJ_HOME=$$YJ_HOME"; \
	case "$$(findmnt -no FSTYPE -T "$$YJ_HOME" 2>/dev/null)" in \
	  tmpfs|ramfs) echo "==> WARNING: $$YJ_HOME is RAM-backed; the search index import needs ~6GB of real disk. Set FRESH_HOME_BASE to a disk-backed path." ;; \
	esac; \
	go tool wails dev -tags webkit2_41 -loglevel Debug -v 2

sandboxes: ## List existing named sandboxes
	@ls -d "$(SANDBOX_DIR)"-* 2>/dev/null \
	  | sed 's|.*/yellowjacket-sandbox-|  |' \
	  || echo "  (none)"

.PHONY: sandbox sandbox-rm sandboxes

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
	go tool golangci-lint run --build-tags indexbuild

# Two passes: the app build, then the `indexbuild` build that adds the
# CI-only dump importer.  Without the second pass nothing would compile
# or exercise backend/explore/dump*.go or cmd/indexbuild at all.
test:
	go test -tags webkit2_41 -race -count=1 -timeout 120s ./...
	go test -tags "webkit2_41 indexbuild" -race -count=1 -timeout 300s \
		./backend/explore/... ./cmd/...

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
