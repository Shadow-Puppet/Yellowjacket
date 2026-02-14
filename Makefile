VERSION ?= dev
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
LDFLAGS := -X 'main.version=$(VERSION)' -X 'main.commit=$(COMMIT)'

dev: generate clean
	WEBKIT_DISABLE_DMABUF_RENDERER=1 wails dev -tags webkit2_41 -loglevel Debug -v 2

build-dev: generate
	wails build -tags webkit2_41 -debug -clean -ldflags "$(LDFLAGS)"

build-prod: generate
	wails build -tags webkit2_41 -clean -obfuscated -upx -ldflags "-s -w $(LDFLAGS)"

build-frontend:
	cd frontend && pnpm install && pnpm build

clean:
	rm -rf frontend/dist
	rm -rf frontend/node_modules

generate:
	go generate ./...

lint:
	golangci-lint run

test:
	go test -tags webkit2_41 -race -count=1 -timeout 120s ./...

vulncheck:
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

setup: ## Install git hooks (lefthook is managed via go.mod tool directive)
	go tool lefthook install
