# Binary name
BINARY=hfdownloader

# Get version from main.go
VERSION=$(shell grep '^const VERSION' main.go | sed -E 's/.*= *"([^"]+)".*/\1/')

# Build directories
BUILD_DIR=output
BUILD_TMP_DIR=$(BUILD_DIR)/.tmp

# Go build flags
LDFLAGS=-ldflags "-s -w"
GO_BUILD=CGO_ENABLED=0 go build $(LDFLAGS)

# Default target
.PHONY: all
all: clean darwin linux windows arm

# Create build directories
$(BUILD_DIR):
	mkdir -p $(BUILD_DIR)

# Clean build artifacts
.PHONY: clean
clean:
	rm -rf $(BUILD_DIR)

# Update VERSION file
.PHONY: version
version:
	@echo "$(VERSION)" > VERSION
	@echo "Version: $(VERSION)"

# Build for macOS (both AMD64 and ARM64)
.PHONY: darwin
darwin: version | $(BUILD_DIR)
	GOOS=darwin GOARCH=amd64 $(GO_BUILD) -o "$(BUILD_DIR)/$(BINARY)_darwin_amd64_$(VERSION)" main.go
	GOOS=darwin GOARCH=arm64 $(GO_BUILD) -o "$(BUILD_DIR)/$(BINARY)_darwin_arm64_$(VERSION)" main.go
	@echo "Built for macOS (AMD64, ARM64)"

# Build for Linux AMD64
.PHONY: linux
linux: version | $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 $(GO_BUILD) -o "$(BUILD_DIR)/$(BINARY)_linux_amd64_$(VERSION)" main.go
	@echo "Built for Linux (AMD64)"

# Build for Windows AMD64
.PHONY: windows
windows: version | $(BUILD_DIR)
	GOOS=windows GOARCH=amd64 $(GO_BUILD) -o "$(BUILD_DIR)/$(BINARY)_windows_amd64_$(VERSION).exe" main.go
	@echo "Built for Windows (AMD64)"

# Build for ARM architectures
.PHONY: arm
arm: version | $(BUILD_DIR)
	GOOS=linux GOARCH=arm GOARM=7 $(GO_BUILD) -o "$(BUILD_DIR)/$(BINARY)_linux_armv7_$(VERSION)" main.go
	GOOS=linux GOARCH=arm64 $(GO_BUILD) -o "$(BUILD_DIR)/$(BINARY)_linux_arm64_$(VERSION)" main.go
	@echo "Built for ARM (ARMv7, ARM64)"

# Install locally (Unix-like systems only)
.PHONY: install
install: darwin
	cp "$(BUILD_DIR)/$(BINARY)_darwin_$(shell uname -m)_$(VERSION)" /usr/local/bin/$(BINARY)
	@echo "Installed to /usr/local/bin/$(BINARY)"

# Run tests
.PHONY: test
test:
	go test -v ./...

# Show help
.PHONY: help
help:
	@echo "Available targets:"
	@echo "  all      - Build for all platforms (default)"
	@echo "  clean    - Remove build artifacts"
	@echo "  darwin   - Build for macOS (AMD64, ARM64)"
	@echo "  linux    - Build for Linux (AMD64)"
	@echo "  windows  - Build for Windows (AMD64)"
	@echo "  arm      - Build for ARM architectures"
	@echo "  install  - Install locally (Unix-like systems)"
	@echo "  test     - Run tests"
	@echo "  version  - Show current version"
	@echo "  help     - Show this help" 