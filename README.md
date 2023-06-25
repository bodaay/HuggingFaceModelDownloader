# HuggingFace Model Downloader

The HuggingFace Model Downloader is a utility tool for downloading models/datasets from the HuggingFace website. It provides multithreaded downloading capabilities for LFS files and ensures the integrity of the downloaded models by checking their SHA256 checksum. 


## Reason

Git LFS was so slow for me, and I cloudn't find a single binary that I can just run to download any model. In addition, this might be integrated later in my future projects for inference using golang/python combination
##One liner Install and Run examples (linux/mac)


## Quick Download and Run Exmaples (linux/mac) only

the bash script will just download the binary based on your os/arch and run it

Download Model: TheBloke/orca_mini_7B-GPTQ
```bash
bash <(curl -sSL https://g.bodaay.io/hfd) -m TheBloke/orca_mini_7B-GPTQ
```

Download Model: TheBloke/vicuna-13b-v1.3.0-GGML and get GGML Variant: q4_0
```bash
bash <(curl -sSL https://g.bodaay.io/hfd) -m TheBloke/vicuna-13b-v1.3.0-GGML:q4_0
```

Download Model: TheBloke/vicuna-13b-v1.3.0-GGML and get GGML Variants: q4_0,q5_0, and save each one in a separate folder
```bash
bash <(curl -sSL https://g.bodaay.io/hfd) -f -m TheBloke/vicuna-13b-v1.3.0-GGML:q4_0,q5_0
```

Download Model: TheBloke/vicuna-13b-v1.3.0-GGML and save them into /workspace/, 8 connections and get GGML Variant: q4_0,q4_K_S
```bash
bash <(curl -sSL https://g.bodaay.io/hfd) -m TheBloke/vicuna-13b-v1.3.0-GGML:q4_0,q4_K_S -c 8 -s /workspace/
```


## Usage:

  hfdowloader [flags]

## Flags:

`-m, --model string`  
Model/Dataset name (required if dataset not set)

You can supply filters for required LFS model files, separate filters by adding commas

filters will discard any LFS file ending with .bin,.act,.safetensors,.zip thats missing the supplied filtercd out 
```bash
-m TheBloke/WizardLM-Uncensored-Falcon-7B-GGML:fp16 # this will download LFS file contains: fp16
```
```bash
-m TheBloke/WizardLM-33B-V1.0-Uncensored-GGML:q4_K_S,q5_K_M # this will download LFS file contains: q4_K_S  or  q5_K_M
```
`-d, --dataset string`  
Model/Dataset name (required if model not set)

`-f, --appendFilterFolder bool`  
Append the filter name to the folder, use it for GGML ONLY qunatizatized filterd download only (optional)

```bash
 # this will download LFS file contains: q4_K_S  or  q5_K_M, in a separate folders by appending the filder name to model folder name
 # all other non-lfs files and not ending with one of these extension: .bin,.safetensors,.meta,.zip will be availale in each folder
-f -m TheBloke/WizardLM-33B-V1.0-Uncensored-GGML:q4_K_S,q5_K_M
```

`-b, --branch string`  
Model/Dataset branch (optional) (default "main")

`-s, --storage string`  
Destination path (optional) (default "Storage")

`-c, --concurrent int`  
Number of LFS concurrent connections (optional) (default 5)

`-t, --token string`  
HuggingFace Access Token, required for some Models/Datasets, you still need to manually accept agreement on HuggingFace if model requires it,No bypass going to be implented (optional)

`-h, --help`  
Help for hfdowloader



Model Example
```shell
hfdowloader  -m TheBloke/WizardLM-13B-V1.0-Uncensored-GPTQ -c 10 -s MyModels
```

Dataset Example
```shell
hfdowloader  -d facebook/flores -c 10 -s MyDatasets
```



## Features
- Nested File Downloading of the Model
- Multithreaded downloading of large files (LFS)
- Filter Downloads, specic LFS models files can be specified for downloading (Usefull for GGMLs), saving time and space
- Simple utlity that can used as library easily or just a single binary, all functionality in one go file and can be imported in any project
- SHA256 checksum verification for LFS downloaded models
- Skipping previsouly downloaded files
- Simple File size matching for non-LFS files
- Support HuggingFace Access Token, for restricted models/datasets



