BINARY_NAME=hfdownloader
OUTPUT_DIR=output

# Extract version from main.go
VERSION=$(shell grep 'var Version' main.go | awk -F\" '{print $$2}')

# Default targets (what `make` runs)
DEFAULT_PLATFORMS=\
	linux/amd64 \
	windows/amd64 \
	darwin/arm64

# Full list (optional if you want to build all at once)
ALL_PLATFORMS=\
	linux/amd64 \
	linux/arm \
	linux/arm64 \
	darwin/amd64 \
	darwin/arm64 \
	windows/amd64

all: update-version $(DEFAULT_PLATFORMS)

# Update VERSION file from main.go
update-version:
	@echo $(VERSION) > VERSION
	@echo "Updated VERSION file to $(VERSION)"

# Pattern rule for cross-compilation
$(ALL_PLATFORMS):
	@mkdir -p $(OUTPUT_DIR)
	@GOOS=$(word 1,$(subst /, ,$@)) GOARCH=$(word 2,$(subst /, ,$@)) \
		CGO_ENABLED=0 go build -o \
		$(OUTPUT_DIR)/$(BINARY_NAME)_$(word 1,$(subst /, ,$@))_$(word 2,$(subst /, ,$@))_$(VERSION)$(if $(findstring windows,$@),.exe,) main.go
	@echo "Build completed: $(OUTPUT_DIR)/$(BINARY_NAME)_$(word 1,$(subst /, ,$@))_$(word 2,$(subst /, ,$@))_$(VERSION)$(if $(findstring windows,$@),.exe,)"

clean:
	rm -rf $(OUTPUT_DIR)

# Convenience target: build everything
build-all: update-version $(ALL_PLATFORMS)
