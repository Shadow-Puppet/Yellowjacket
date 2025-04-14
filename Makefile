dev:
	WEBKIT_DISABLE_DMABUF_RENDERER=1 wails dev -tags webkit2_41

build-dev:
	wails build -tags webkit2_41 -debug -clean

build-prod:
	wails build -tags webkit2_41 -clean -obfuscated -upx

generate:
	go generate ./...
