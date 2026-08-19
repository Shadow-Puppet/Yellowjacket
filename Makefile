VERSION ?= dev
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
LDFLAGS := -X 'main.version=$(VERSION)' -X 'main.commit=$(COMMIT)'

# YJ_HOME isolates the dev build's config + database from a packaged
# install. Defaults to a sandbox under XDG data; override in .env to
# point elsewhere (or unset it there to share the real user dirs).
DEV_YJ_HOME ?= $(HOME)/.local/share/yellowjacket-dev

# `wails3 dev` and `wails3 task` run the scaffold's Taskfile tree, which
# invokes `wails3` by bare name.  The CLI is a vendored Go tool, so the
# name only exists on PATH via this shim -- see scripts/toolbin/wails3.
# Without it every supervisor target dies with
# "/bin/sh: wails3: command not found" at its first sub-task.
TOOLBIN := $(CURDIR)/scripts/toolbin

dev: setup generate clean
	if [ -f .env ]; then set -a; . ./.env; set +a; fi; : "$${YJ_HOME:=$(DEV_YJ_HOME)}"; export YJ_HOME; PATH="$(TOOLBIN):$$PATH" go tool wails3 dev -config ./build/config.yml

dev-debug: setup generate clean
	if [ -f .env ]; then set -a; . ./.env; set +a; fi; : "$${YJ_HOME:=$(DEV_YJ_HOME)}"; export YJ_HOME; YJ_LOG_LEVEL=debug PATH="$(TOOLBIN):$$PATH" go tool wails3 dev -config ./build/config.yml

# ── Headless harness (plan 005) ──────────────────────────────────────
# The same app `make dev` runs, minus the window: v3's `-tags server`
# is a first-class headless mode that needs no display at all, so the
# Xvfb this used to require is gone.  The script returns once :34115
# answers.  This is the only entry point an agent can use, since every
# other one blocks the terminal forever.
dev-headless: ## Start the app headless in the background (SEED=<name> to seed)
	@./scripts/dev-headless.sh $(if $(SEED),--seed $(SEED),) $(HEADLESS_ARGS)

dev-headless-fresh: ## Same, but on an empty YJ_HOME (first-run wizard)
	@./scripts/dev-headless.sh --fresh $(HEADLESS_ARGS)

dev-stop: ## Stop the headless app (SIGTERM, so shutdown hooks run)
	@./scripts/dev-stop.sh

dev-logs: ## Tail the headless app log
	@tail -f .dev/app.log

# ---------------------------------------------------------------- #
# The Android tier.  See .pi/skills/yellowjacket-dev/references/     #
# android-tier.md for which of these to reach for and why a failure  #
# here looks like nothing at all.                                    #
# ---------------------------------------------------------------- #

# The NDK is pinned: r26d is what the pipeline is built and checked
# against, and newer NDKs have broken Wails' Android build before.
# ANDROID_HOME must carry a *platform*, which Arch's /opt/android-sdk
# does not — hence the separate default.
ANDROID_SDK ?= $(HOME)/Android/Sdk
ANDROID_NDK ?= /opt/android-ndk
ANDROID_ENV := ANDROID_HOME=$(ANDROID_SDK) ANDROID_SDK_ROOT=$(ANDROID_SDK) ANDROID_NDK_HOME=$(ANDROID_NDK)

# `package`, not `package:fat`: x86_64 Android cannot run this app at
# all (modernc's raw lstat vs Android's seccomp -- see
# android-tier.md), so the second ABI was ~31 MB that could not run
# anywhere.  app/build.gradle's abiFilters says the same thing to
# Gradle; both have to agree or the .so is built and then dropped.
android: build-frontend ## Build the arm64 APK into bin/
	@$(ANDROID_ENV) PATH="$(TOOLBIN):$$PATH" go tool wails3 task android:package

android-setup: ## Install the SDK pieces and create the AVD (once, ~3.5GB)
	@$(ANDROID_ENV) ./scripts/android-emulator.sh setup

android-emulator: ## Boot the emulator headless in the background and wait for it
	@$(ANDROID_ENV) ./scripts/android-emulator.sh start

android-emulator-stop: ## Shut the emulator down (console kill, then saved PID)
	@$(ANDROID_ENV) ./scripts/android-emulator.sh stop

android-install: ## Install bin/yellowjacket.apk onto the running emulator
	@$(ANDROID_ENV) ./scripts/android-emulator.sh install

android-launch: ## Force-stop, clear logcat, and start the app
	@$(ANDROID_ENV) ./scripts/android-emulator.sh launch

android-logs: ## Tail logcat, filtered to the app's own tags
	@$(ANDROID_ENV) ./scripts/android-emulator.sh logs

# The only tier that can see the platform is the one you can look at.
android-screenshot: ## Grab the device screen (OUT=<path>)
	@$(ANDROID_ENV) ./scripts/android-emulator.sh screenshot $(OUT)

# The page's own answer, from the engine that is really rendering it.
# Needs the debug build installed (it is a sibling id, so it does not
# disturb the release app): see scripts/android-eval.mjs.
android-inspect: ## Forward the device WebView's devtools socket
	@$(ANDROID_ENV) ./scripts/android-emulator.sh inspect

android-eval: ## Evaluate JS in the device WebView (EXPR='...')
	@node ./scripts/android-eval.mjs $(if $(EXPR),'$(EXPR)',)

# "Did it start" is the wrong question — a crash-looping app starts
# several times a second.  This asserts the *same pid* is still there.
android-smoke: ## Launch and assert the app is still alive (SECONDS=<n>)
	@$(ANDROID_ENV) ./scripts/android-emulator.sh smoke $(if $(SECONDS),$(SECONDS),10)

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

# Bindings are generated by `wails3`, NOT by `go generate`, so the
# pre-commit codegen check does not cover them: a renamed Go struct
# field would otherwise surface at runtime, inside a window.
bindings-check: ## Fail if the generated bindings are stale
	@./scripts/bindings-check.sh

# A backtick inside a comment in a css`` literal ends the literal, and
# what you get back is a type error about CSSResult, or every test in
# the suite failing to import. Four sessions, three plans. Instant.
.PHONY: css-check
css-check: ## Fail if a css`` literal was ended early by a backtick in a comment
	@cd frontend && node scripts/check-css-literals.mjs

# .pi/ and CLAUDE.md document commands, and a doc that documents a
# command wrongly is worse than no doc: an agent runs it confidently.
# Every command in them is a make target on purpose, so this is
# checkable.  It also asserts AGENTS.md is a symlink to CLAUDE.md, so the
# two harnesses cannot drift onto two descriptions of one project.
skill-check: ## Fail if the agent docs name a missing make target, or AGENTS.md is not a symlink
	@./scripts/skill-check.sh

# Conventional Commits, which CLAUDE.md claimed CI enforced for a long
# time before anything did.  RANGE=A..B lints a push; bare lints HEAD.
commit-check: ## Fail if a commit subject is not a Conventional Commit
	@./scripts/commit-check.sh $(if $(RANGE),--range $(RANGE))

# What running the release workflow now would ship, without shipping it.
# Reads the same .releaserc.yml CI does, so "why did that not cut a
# version" is answerable locally instead of by pushing and watching.
# Needs no credentials: --dry-run neither tags nor publishes.
#
# release.yml is dispatch-only, so this answers the question that
# actually gets asked now -- what has accumulated since the last tag --
# rather than what one merge would have done.  The workflow's own
# `dry_run` input is the same answer from the runner, against whatever
# main points at rather than the working tree.
#
# The pins must stay identical to release.yml's, which is where the note
# on holding the conventionalcommits preset at 9 lives -- at 10 the
# release notes come out empty with everything green.
release-dry: ## Print the version a release run would cut right now
	@npx --yes \
		-p semantic-release@25 \
		-p @semantic-release/commit-analyzer@13 \
		-p @semantic-release/release-notes-generator@14 \
		-p @semantic-release/changelog@7 \
		-p @semantic-release/exec@7 \
		-p conventional-changelog-conventionalcommits@9 \
		semantic-release --dry-run --no-ci

# v3 generates TypeScript into frontend/bindings/, nested by Go import
# path, rather than v2's frontend/wailsjs/.  The `@go` alias absorbs the
# constant prefix, so a call site imports '@go/library/library.js'.
#
# No -f flag: the tag set is the default one, deliberately, because the
# generator is a static analyser that sees only the configuration it is
# told about and the one that matters is the one users run.  See
# scripts/bindings-check.sh for why the other two do not apply.
bindings: ## Regenerate frontend/bindings from the bound Go services
	go tool wails3 generate bindings -clean=true -ts -i

.PHONY: dev-headless dev-headless-fresh dev-stop dev-logs \
	sandbox-seed sandbox-seed-bulk sandbox-seeds e2e e2e-setup e2e-report \
	perf perf-compare \
	ui-test ui-watch ui-visual ui-visual-update ui-setup \
	bindings bindings-check skill-check commit-check release-dry

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
	PATH="$(TOOLBIN):$$PATH" go tool wails3 dev -config ./build/config.yml

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
	PATH="$(TOOLBIN):$$PATH" go tool wails3 dev -config ./build/config.yml

sandboxes: ## List existing named sandboxes
	@ls -d "$(SANDBOX_DIR)"-* 2>/dev/null \
	  | sed 's|.*/yellowjacket-sandbox-|  |' \
	  || echo "  (none)"

.PHONY: sandbox sandbox-rm sandboxes

build-dev: generate
	PATH="$(TOOLBIN):$$PATH" go tool wails3 task build DEV=true

build-prod: generate
	PATH="$(TOOLBIN):$$PATH" go tool wails3 task build

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
# configurations that nothing builds.  The webkit2_41 tag these all used
# to carry is gone with v2: v3 builds against GTK4 + WebKitGTK 6.0 by
# default, which both Arch and ubuntu:24.04 ship, so the default tag set
# is the one that ships.  (`-tags gtk3` still exists as an escape hatch
# for a machine without webkitgtk-6.0; it is not what CI or releases
# build.)
lint:
	go tool golangci-lint run
	go tool golangci-lint run --build-tags indexbuild
	go tool golangci-lint run --build-tags dev

# Three passes: the app build, the `indexbuild` build that adds the
# CI-only dump importer, and the `dev` build that adds profiling and
# backend/testctl.  Without the extra passes nothing would compile or
# exercise backend/explore/dump*.go, cmd/indexbuild or the harness
# control surface at all.
test: testdata
	go test -race -count=1 -timeout 120s ./...
	go test -tags indexbuild -race -count=1 -timeout 300s \
		./backend/explore/... ./cmd/...
	# backend/testctl only exists under the `dev` tag, so the pass above
	# does not compile it, let alone run it.
	go test -tags dev -race -count=1 -timeout 120s \
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
