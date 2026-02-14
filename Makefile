dev: generate clean
	WEBKIT_DISABLE_DMABUF_RENDERER=1 wails dev -tags webkit2_41 -loglevel Debug -v 2

build-dev: generate
	wails build -tags webkit2_41 -debug -clean

build-prod: generate
	wails build -tags webkit2_41 -clean -obfuscated -upx

build-frontend:
	cd frontend && pnpm build

clean: 
	rm -rf frontend/dist
	rm -rf frontend/node_modules

generate:
	go generate ./...
