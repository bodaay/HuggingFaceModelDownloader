# HuggingFace Fast Downloader

A fast and efficient tool for downloading files from HuggingFace repositories. Features parallel downloads, SHA verification, and flexible file filtering.

## Features

- Fast parallel downloads with configurable connections
- SHA verification for file integrity
- Flexible file filtering with glob and regex patterns
- Custom destination paths for downloaded files
- Support for private repositories with token authentication
- Tree view of repository contents

## Installation

### From Source

```bash
# Clone the repository
git clone https://github.com/yourusername/hfdownloader
cd hfdownloader

# Build for your platform
make        # Build for all platforms
make darwin # macOS only (AMD64, ARM64)
make linux  # Linux only (AMD64)
make arm    # ARM only (ARMv7, ARM64)

# Install locally (Unix-like systems)
sudo make install
```

For more build options, run `make help`.

### Using Go Install

```bash
go install github.com/yourusername/hfdownloader@latest
```

## Usage

### List Repository Contents

```bash
# List all files in a repository
hfdownloader -r runwayml/stable-diffusion-v1-5 list

# List files in a specific branch/commit
hfdownloader -r runwayml/stable-diffusion-v1-5 list -b main
```

### Practical Examples

Here are some real-world examples using a FP8 model repository:

```bash
# List all files in the repository
hfdownloader -r Kijai/flux-fp8 list

# Download all the vae safetensor files into the current directory
hfdownloader -r Kijai/flux-fp8 download -f "*vae*.safetensors" 

# Download VAE model to models/vae directory (auto-confirm)
hfdownloader -r Kijai/flux-fp8 download -f "*vae*.safetensors:models/vae" -y

# Same as above but with 16 concurrent connections for faster download
hfdownloader -r Kijai/flux-fp8 download -f "*vae*.safetensors:models/vae" -y -c 16

# Use a regex instead of glob and skip SHA verification for faster downloads
hfdownloader -r Kijai/flux-fp8 download -f "/e4m3fn/:models/checkpoints" -y -c 16 --skip-sha
```

### Authentication

For private repositories, you can provide your HuggingFace token:

```bash
hfdownloader -r private-org/model download -t "your_token_here" -f "*.safetensors"
```

## Pattern Matching

The downloader supports two types of patterns:

1. Glob Patterns (default):
   - `*.safetensors` - match all safetensors files
   - `model/*.bin` - match bin files in model directory
   - `v2-*/*.ckpt` - match ckpt files in v2-* directories

2. Regex Patterns (enclosed in /):
   - `/\\.safetensors$/` - match files ending in .safetensors
   - `/v\\d+/.*\\.bin$/` - match .bin files in version directories
   - `/model_(fp16|fp32)\\.bin$/` - match specific model variants

## Destination Mapping

You can specify custom destinations for downloaded files using the format `pattern:destination`. The destination can be specified in three ways:

1. Directory with trailing slash (`path/to/dir/`):
   ```bash
   # Downloads flux-vae.safetensors to models/vae/flux-vae.safetensors
   hfdownloader -r org/model download -f "flux-vae.safetensors:models/vae/"
   
   # Downloads all .safetensors files to models/checkpoints/, keeping original names
   hfdownloader -r org/model download -f "*.safetensors:models/checkpoints/"
   
   # Downloads multiple files to different directories
   hfdownloader -r org/model download \
     -f "model.safetensors:models/full/" \
     -f "vae/*.pt:models/vae/" \
     -f "configs/*.yaml:configs/"
   ```

2. Existing directory (without trailing slash):
   ```bash
   # If models/vae exists, this will show a warning and download to:
   # models/vae/flux-vae.safetensors
   hfdownloader -r org/model download -f "flux-vae.safetensors:models/vae"
   
   # Multiple files to existing directory
   hfdownloader -r org/model download \
     -f "*-fp16.safetensors:models/checkpoints" \
     -f "*-fp32.safetensors:models/checkpoints"
   ```

3. Full file path (new filename):
   ```bash
   # Downloads to exact path with new filename
   hfdownloader -r org/model download \
     -f "model.safetensors:models/checkpoints/sd15-base.safetensors"
   
   # Multiple files with custom names
   hfdownloader -r org/model download \
     -f "model-v1.safetensors:models/v1-base.safetensors" \
     -f "model-v2.safetensors:models/v2-base.safetensors"
   ```

### Complex Examples

1. Mix of patterns and destinations:
   ```bash
   # Download multiple file types to organized directories
   hfdownloader -r org/model download \
     -f "*.safetensors:models/" \
     -f "*.pt:weights/" \
     -f "*.yaml:configs/" \
     -f "*.json:configs/"
   ```

2. Using regex with custom destinations:
   ```bash
   # Download specific model variants to organized directories
   hfdownloader -r org/model download \
     -f "/model_fp16.*/:models/fp16/" \
     -f "/model_fp32.*/:models/fp32/" \
     -f "/vae_v[0-9].*/:models/vae/"
   ```

3. Combining glob patterns with specific paths:
   ```bash
   # Download and rename some files, keep original names for others
   hfdownloader -r org/model download \
     -f "model.safetensors:models/sd15-base.safetensors" \
     -f "vae/*.pt:models/vae/" \
     -f "embeddings/*.pt:embeddings/" \
     -f "lora/*.safetensors:models/lora/"
   ```

4. Using patterns with directory structure:
   ```bash
   # Match nested directory structure
   hfdownloader -r org/model download \
     -f "v1/*/*.safetensors:models/v1/" \
     -f "v2/*/*.safetensors:models/v2/" \
     -f "*/vae/*.pt:models/vae/"
   ```

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

MIT License - see LICENSE file for details
