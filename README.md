# HuggingFace Model Downloader

The HuggingFace Model Downloader is a utility tool for downloading models and datasets from the HuggingFace website. It offers multithreaded downloading for LFS files and ensures the integrity of downloaded models with SHA256 checksum verification.

## Reason

Git LFS was slow for me, and I couldn't find a single binary for easy model downloading. This tool may also be integrated into future projects for inference using a Go/Python combination.

## One Line Installer (Linux/Mac/Windows WSL2)

The script downloads the correct version based on your OS/architecture and saves the binary as "hfdownloader" in the current folder.

```shell
bash <(curl -sSL https://g.bodaay.io/hfd) -h
```

To install it to the default OS bin folder:

```shell
bash <(curl -sSL https://g.bodaay.io/hfd) -i
```

It will automatically request higher 'sudo' privileges if required. You can specify the install destination with `-p`.

```shell
bash <(curl -sSL https://g.bodaay.io/hfd) -i -p ~/.local/bin/
```

## Quick Download and Run Examples (Linux/Mac/Windows WSL2)

The bash script just downloads the binary based on your OS/architecture and runs it.

### Download Model: TheBloke/orca_mini_7B-GPTQ

```shell
bash <(curl -sSL https://g.bodaay.io/hfd) -m TheBloke/orca_mini_7B-GPTQ
```

### Download Model: TheBloke/vicuna-13b-v1.3.0-GGML and Get GGML Variant: q4_0

```shell
bash <(curl -sSL https://g.bodaay.io/hfd) -m TheBloke/vicuna-13b-v1.3.0-GGML:q4_0
```

### Download Model: TheBloke/vicuna-13b-v1.3.0-GGML and Get GGML Variants: q4_0,q5_0 in Separate Folders

```shell
bash <(curl -sSL https://g.bodaay.io/hfd) -f -m TheBloke/vicuna-13b-v1.3.0-GGML:q4_0,q5_0
```

### Download Model with 8 Connections and Save into /workspace/

```shell
bash <(curl -sSL https://g.bodaay.io/hfd) -m TheBloke/vicuna-13b-v1.3.0-GGML:q4_0,q4_K_S -c 8 -s /workspace/
```

## Usage

```shell
hfdownloader [flags]
```

## Flags

- `-m, --model string`: Model/Dataset name (required if dataset not set). You can supply filters for required LFS model files. Filters will discard any LFS file ending with .bin, .act, .safetensors, .zip that are missing the supplied filtered out.
- `-d, --dataset string`: Dataset name (required if model not set).
- `-f, --appendFilterFolder bool`: Append the filter name to the folder, use it for GGML quantized filtered download only (optional).
- `-k, --skipSHA bool`: Skip SHA256 checking for LFS files, useful when trying to resume interrupted downloads and complete missing files quickly (optional).
- `-b, --branch string`: Model/Dataset branch (optional, default "main").
- `-s, --storage string`: Storage path (optional, default "Storage").
- `-c, --concurrent int`: Number of LFS concurrent connections (optional, default 5).
- `-t, --token string`: HuggingFace Access Token, can be supplied by env variable 'HUGGING_FACE_HUB_TOKEN' or .env file (optional).
- `-i, --install bool`: Install the binary to the OS default bin folder, Unix-like operating systems only.
- `-p, --installPath string`: Specify install path, used with `-i` (optional).
- `-h, --help`: Help for hfdownloader.

## Examples

### Model Example

```shell
hfdownloader -m TheBloke/WizardLM-13B-V1.0-Uncensored-GPTQ -c 10 -s MyModels
```

### Dataset Example

```shell
hfdownloader -d facebook/flores -c 10 -s MyDatasets
```

## Features

- Nested file downloading of the model
- Multithreaded downloading of large files (LFS)
- Filter downloads for specific LFS model files (useful for GGML/GGUFs)
- Simple utility that can be used as a library or a single binary
- SHA256 checksum verification for downloaded models
- Skipping previously downloaded files
- Resume progress for interrupted downloads
- Simple file size matching for non-LFS files
- Support for HuggingFace Access Token for restricted models/datasets
- Configuration File Support: You can now create a configuration file at `~/.config/hfdownloader.json` to set default values for all command flags.
- Generate Configuration File: A new command `hfdownloader generate-config` generates an example configuration file with default values at the above path.
- Existing downloads will be updated if the model/dataset already exists in the storage path and new files or versions are available.
