# HuggingFace Model Downloader

The HuggingFace Model Downloader is a utility tool for downloading models from the HuggingFace website. It provides multithreaded downloading capabilities for LFS files and ensures the integrity of the downloaded models by checking their SHA256 checksum. 


## Reason

Git LFS was so slow for me, and I cloudn't find a single binary that I can just run to download any model. In addition, this might be integrated later in my future projects for inference using golang/python combination

## Usage:

  hfdowloader [flags]


Flags:
- `-m, --model-name string`: Model name (required, pattern: ModelAuthor/ModelName)
- `-d, --destination-path string`: Destination path (optional, default: "Models")
- `-c, --concurrent int`: Number of LFS concurrent connections (optional, default: 5)
- `-b, --model-branch string`: Model branch (optional, default: "main")


```shell
hfdowloader  -m TheBloke/WizardLM-13B-V1.0-Uncensored-GPTQ -c 10 -d MyModels
```



## Features
- Nested File Downloading of the Model
- Multithreaded downloading of large files (LFS)
- Simple utlity that can used as library easily or just a single binary, all functionality in one go file and can be imported in any project
- SHA256 checksum verification for LFS downloaded models
- Skipping previsouly downloaded files
- Simple File size matching for non-LFS files



