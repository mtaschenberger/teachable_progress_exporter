APP_NAME := bxp-assessment-evaluation
WAILS ?= wails
GO ?= go
GO_ENV := GOCACHE=$(CURDIR)/.gocache GOMODCACHE=$(CURDIR)/.gomodcache

.PHONY: help tidy dev build build-windows build-windows-arm64 build-linux build-macos build-all bindings clean

help:
	@echo "Targets:"
	@echo "  make tidy              - Download/resolve Go modules"
	@echo "  make bindings          - Generate Wails JS bindings"
	@echo "  make dev               - Run app in Wails dev mode"
	@echo "  make build             - Build app for current OS"
	@echo "  make build-windows     - Cross-build app for Windows amd64"
	@echo "  make build-windows-arm64 - Cross-build app for Windows arm64"
	@echo "  make build-linux       - Cross-build app for Linux amd64"
	@echo "  make build-macos       - Build app for macOS universal"
	@echo "  make build-all         - Build for macOS + Linux + Windows"
	@echo "  make clean             - Remove local build caches"

tidy:
	@mkdir -p .gocache .gomodcache
	$(GO_ENV) $(GO) mod tidy

bindings: tidy
	$(GO_ENV) $(WAILS) generate bindings

dev: tidy
	$(GO_ENV) $(WAILS) dev

build: tidy
	$(GO_ENV) $(WAILS) build -clean

# Note: Cross-platform Wails builds require platform-specific toolchains on the host.
build-windows: tidy
	$(GO_ENV) $(WAILS) build -clean -platform windows/amd64

build-windows-arm64: tidy
	$(GO_ENV) $(WAILS) build -clean -platform windows/arm64

build-linux: tidy
	$(GO_ENV) $(WAILS) build -clean -platform linux/amd64

build-macos: tidy
	$(GO_ENV) $(WAILS) build -clean -platform darwin/universal

build-all: build-macos build-linux build-windows

clean:
	rm -rf .gocache .gomodcache
