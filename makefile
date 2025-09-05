
# Static cross-compile Makefile for HuggingFaceModelDownloader
# Produces fully static binaries (no dynamic libs) where supported.
# Notes:
#   - CGO is disabled; binaries are pure Go.
#   - On non-darwin targets we pass -extldflags '-static' to force static linking.
#   - On darwin, fully static linking of system libs is not supported; with CGO=0
#     the resulting binaries are still pure-Go and do not depend on dynamic C libs.

BINARY_NAME := hfdownloader
OUTPUT_DIR  := output

# Grab Version string from main.go (expects: var Version = "x.y.z")
VERSION := $(shell awk -F\" '/var[[:space:]]+Version/ {print $$2}' main.go)

# All Go sources (adjust if you keep sources outside the root)
SOURCES := $(wildcard *.go)

# Default platforms built by `make` or `make build`
DEFAULT_PLATFORMS := \
	linux/amd64 \
	windows/amd64 \
	darwin/arm64

# A bigger set for convenience: `make build-all`
ALL_PLATFORMS := \
	linux/amd64 \
	linux/386 \
	linux/arm \
	linux/arm64 \
	linux/ppc64le \
	linux/s390x \
	windows/amd64 \
	windows/386 \
	windows/arm64 \
	darwin/amd64 \
	darwin/arm64 \
	freebsd/amd64 \
	freebsd/arm64

# Common ldflags: strip symbols and embed Version
LDFLAGS_COMMON := -s -w

.PHONY: all build build-all clean info _build_many _one help

all: build

## Build default platforms (static)
build:
	@$(MAKE) --no-print-directory _build_many PLATFORMS="$(DEFAULT_PLATFORMS)"

## Build extended list of platforms (static)
build-all:
	@$(MAKE) --no-print-directory _build_many PLATFORMS="$(ALL_PLATFORMS)"

# Internal: iterate over $(PLATFORMS) and dispatch per-platform builds
_build_many:
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; \
		$(MAKE) --no-print-directory _one GOOS=$$os GOARCH=$$arch; \
	done

# Internal: build a single (GOOS, GOARCH) pair with static linking
_one:
	@mkdir -p "$(OUTPUT_DIR)"
	@out="$(OUTPUT_DIR)/$(BINARY_NAME)_$(GOOS)_$(GOARCH)_$(VERSION)"; \
	ext=""; [ "$(GOOS)" = "windows" ] && ext=".exe"; \
	ldflags="$(LDFLAGS_COMMON) -X 'main.Version=$(VERSION)'"; \
	if [ "$(GOOS)" != "darwin" ]; then ldflags="$$ldflags -extldflags '-static'"; fi; \
	echo "=> Building $(GOOS)/$(GOARCH) (static)"; \
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) \
		go build -trimpath -ldflags "$$ldflags $(LDFLAGS_EXTRA)" -o "$$out$$ext" $(SOURCES); \
	echo "   done: $$out$$ext"

## Remove build artifacts
clean:
	rm -rf "$(OUTPUT_DIR)"

## Print detected version and sources
info:
	@echo "Version : $(VERSION)"; \
	echo "Sources : $(SOURCES)"; \
	echo "Default : $(DEFAULT_PLATFORMS)"; \
	echo "All     : $(ALL_PLATFORMS)"

## Show help
help:
	@awk '/^##/ {sub(/^## /, ""); help=$$0; getline; sub(/:.*/, "", $$1); printf "  %-12s %s\n", $$1, help }' $(MAKEFILE_LIST)
