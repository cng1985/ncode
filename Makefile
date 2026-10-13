GO ?= go
NPM ?= npm
NPM_CACHE ?= /tmp/ncode-npm-cache

.PHONY: web-deps web-build build test

web-deps:
	cd web && $(NPM) ci --cache=$(NPM_CACHE)

web-build:
	@if [ ! -x web/node_modules/.bin/vite ]; then $(MAKE) web-deps; fi
	cd web && $(NPM) run build

build: web-build
	mkdir -p bin
	$(GO) build -o bin/ncode ./cmd/api

test: web-build
	$(GO) test ./...
