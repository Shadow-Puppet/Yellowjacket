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

# ── Headless harness (plan 005) ──────────────────────────────────────
# The same dev server `make dev` runs, minus the blocking GTK window:
# Xvfb gives it the display it insists on, and the script returns once
# :34115 answers.  This is the only entry point an agent can use, since
# every other one blocks the terminal forever.
dev-headless: ## Start the app headless in the background (SEED=<name> to seed)
	@./scripts/dev-headless.sh $(if $(SEED),--seed $(SEED),) $(HEADLESS_ARGS)

dev-headless-fresh: ## Same, but on an empty YJ_HOME (first-run wizard)
	@./scripts/dev-headless.sh --fresh $(HEADLESS_ARGS)

dev-stop: ## Stop the headless app (SIGTERM, so shutdown hooks run)
	@./scripts/dev-stop.sh

dev-logs: ## Tail the headless app log
	@tail -f .dev/app.log

# Seeds are produced by *running the app* — driving the real AddLibrary
# binding and waiting for the real scan — never by hand-writing a
# config.toml and DB rows.  A hand-built seed is a second description
# of a valid YJ_HOME and would drift from the real one.
sandbox-seed: testdata ## Build a seeded YJ_HOME snapshot: make sandbox-seed NAME=<n>
	@./scripts/seed-sandbox.sh $(if $(NAME),--name $(NAME),)

# The bulk seed is the *measurement* seed, not a fixture seed.  Same
# script and the same discipline (the app builds it by scanning for
# real); the only difference is which manifest it is pointed at.  It is
# a separate target because a 50 000-track scan is minutes, and nothing
# routine should depend on it.
sandbox-seed-bulk: bulkdata ## Build a seeded YJ_HOME from the bulk library
	@./scripts/seed-sandbox.sh --name $(if $(NAME),$(NAME),bulk) \
		--manifest .dev/music_library_bulk.manifest.json

sandbox-seeds: ## List built seeds
	@ls -1 .dev/seeds/*.tar 2>/dev/null | sed 's|.*/||; s|\.tar$$||' \
		|| echo "  (none; build one with: make sandbox-seed NAME=default)"

# The specs drive the app that is *already* running: `make dev-headless`
# daemonises, which is the opposite of what Playwright's `webServer`
# supervises, and starting one per run would rebuild the frontend every
# time.  globalSetup fails with the exact commands to run if it is down.
# Phase 4 of plan 007 is verified by measurement rather than assertion,
# so this is not a spec and does not run in CI: it produces a number to
# read, against a running app seeded with the bulk library.
#
#   make sandbox-seed-bulk && make dev-headless SEED=bulk
#   make perf LABEL=before   ... change something ...   make perf LABEL=after
#   make perf-compare BEFORE=before AFTER=after
perf: ## Take a performance measurement (LABEL=<name>) of a running app
	@cd e2e && pnpm install --silent && \
		node perf/measure.mjs --label $(if $(LABEL),$(LABEL),current)

perf-compare: ## Print a before/after table: BEFORE=<a> AFTER=<b>
	@cd e2e && node perf/measure.mjs --compare \
		$(if $(BEFORE),$(BEFORE),before) $(if $(AFTER),$(AFTER),after)

e2e: ## Run the Playwright smoke suite against a running dev-headless app
	@cd e2e && pnpm install --silent && npx playwright test $(E2E_ARGS)

e2e-setup: ## Install the e2e runner and its browser (once)
	@cd e2e && pnpm install && npx playwright install chromium

e2e-report: ## Open the HTML report from the last e2e run
	@cd e2e && npx playwright show-report

# The cheapest tier: components and stores in a real browser, with no
# Wails, no backend, no seeded library and no virtual display.  Lives in
# frontend/ rather than e2e/ so the Vitest browser provider and the
# Playwright runner cannot fight over versions or globs.
ui-test: ## Run the Vitest component and store suite (frontend/)
	@cd frontend && pnpm install --silent && npx vitest run $(UI_ARGS)

ui-watch: ## Same suite, in watch mode
	@cd frontend && npx vitest

# Visual regression is opt-in: toMatchScreenshot baselines depend on
# font hinting and compositing, so they only mean anything on the
# machine (or container) that took them.
ui-visual: ## Run the suite including screenshot comparisons
	@cd frontend && YJ_VISUAL=1 npx vitest run $(UI_ARGS)

ui-visual-update: ## Re-record the screenshot baselines
	@cd frontend && YJ_VISUAL=1 npx vitest run --update $(UI_ARGS)

ui-setup: ## Install the Vitest browser provider's own Chromium (once)
	@cd frontend && pnpm install && npx playwright install chromium

# frontend/wailsjs is generated by `wails`, NOT by `go generate`, so the
# pre-commit codegen check does not cover it: a renamed Go struct field
# currently surfaces at runtime, in a window.  File modes are ignored
# because `wails generate module` rewrites the runtime files as 755.
bindings-check: ## Fail if frontend/wailsjs is stale against the Go bindings
	@./scripts/bindings-check.sh

# A backtick inside a comment in a css`` literal ends the literal, and
# what you get back is a type error about CSSResult, or every test in
# the suite failing to import. Four sessions, three plans. Instant.
.PHONY: css-check
css-check: ## Fail if a css`` literal was ended early by a backtick in a comment
	@cd frontend && node scripts/check-css-literals.mjs

# .pi/ documents commands, and a skill that documents a command wrongly
# is worse than no skill: an agent runs it confidently.  Every command
# in there is a make target on purpose, so this is checkable.
skill-check: ## Fail if .pi/ documents a make target that does not exist
	@./scripts/skill-check.sh

# Conventional Commits, which CLAUDE.md claimed CI enforced for a long
# time before anything did.  RANGE=A..B lints a push; bare lints HEAD.
commit-check: ## Fail if a commit subject is not a Conventional Commit
	@./scripts/commit-check.sh $(if $(RANGE),--range $(RANGE))

bindings: ## Regenerate frontend/wailsjs from the bound Go structs
	go tool wails generate module -tags webkit2_41
	@chmod 644 frontend/wailsjs/runtime/runtime.js \
		frontend/wailsjs/runtime/runtime.d.ts \
		frontend/wailsjs/runtime/package.json

.PHONY: dev-headless dev-headless-fresh dev-stop dev-logs \
	sandbox-seed sandbox-seed-bulk sandbox-seeds e2e e2e-setup e2e-report \
	perf perf-compare \
	ui-test ui-watch ui-visual ui-visual-update ui-setup \
	bindings bindings-check skill-check commit-check

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

# The fixture library is generated, not committed: deterministic audio
# across all four supported formats, tagged by backend/tagwriter so the
# fixtures and the reader under test cannot drift.  Regenerates only
# when the spec's manifest hash has changed, so it is cheap to depend on.
testdata: ## Generate the deterministic fixture music library
	go run ./cmd/gentestdata

testdata-force: ## Regenerate the fixture library unconditionally
	go run ./cmd/gentestdata -force

testdata-clean: ## Delete the generated fixture library
	rm -rf test_data/music_library_test test_data/music_library_broken \
		test_data/music_library_test.manifest.json

# The bulk library answers a different question from the fixture one:
# not "does this behave correctly" but "how does this behave at the
# size the audit measured".  ~11 s, ~470 MB, into a gitignored .dev/,
# and deliberately not a dependency of `make test`.
BULK_TRACKS ?= 50000

bulkdata: ## Generate the bulk measurement library (BULK_TRACKS=50000)
	go run ./cmd/gentestdata -bulk $(BULK_TRACKS)

bulkdata-clean: ## Delete the bulk measurement library
	rm -rf .dev/music_library_bulk .dev/music_library_bulk.manifest.json

.PHONY: testdata testdata-force testdata-clean bulkdata bulkdata-clean

# The tag sets must match `make test` exactly, or lint is checking three
# configurations that nothing builds.  webkit2_41 is not optional: without
# it wails resolves webkit2gtk-4.0, which Ubuntu 24.04 no longer ships, so
# the `dev` pass (wails' own app_dev.go is dev-tagged and pulls in the 4.0
# assetserver) fails to typecheck anywhere but Arch.
lint:
	go tool golangci-lint run --build-tags webkit2_41
	go tool golangci-lint run --build-tags "webkit2_41 indexbuild"
	go tool golangci-lint run --build-tags "webkit2_41 dev"

# Three passes: the app build, the `indexbuild` build that adds the
# CI-only dump importer, and the `dev` build that adds profiling and
# backend/testctl.  Without the extra passes nothing would compile or
# exercise backend/explore/dump*.go, cmd/indexbuild or the harness
# control surface at all.
test: testdata
	go test -tags webkit2_41 -race -count=1 -timeout 120s ./...
	go test -tags "webkit2_41 indexbuild" -race -count=1 -timeout 300s \
		./backend/explore/... ./cmd/...
	# backend/testctl only exists under the `dev` tag, so the pass above
	# does not compile it, let alone run it.
	go test -tags "webkit2_41 dev" -race -count=1 -timeout 120s \
		./backend/testctl/...

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
