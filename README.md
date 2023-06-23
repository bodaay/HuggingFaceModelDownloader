# HuggingFace Model Downloader

The HuggingFace Model Downloader is a utility tool for downloading models from the HuggingFace website. It provides multithreaded downloading capabilities and ensures the integrity of the downloaded models by checking their SHA256 checksum. 


## Reason

Git LFS was so slow for me, and I cloudn't find a single binary that I can just run to download any model. In addition, this might be integrated later in my future projects for inference using golang/python combination

## Features
- Nested File Downloading of the Model
- Simple utlity that can used as library easily or just a single binary, all functionality in one go file and can be imported in any project
- SHA256 checksum verification for LFS downloaded models
- Simple File size matching for non-LFS files



