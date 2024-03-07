#!/bin/bash

#this already uploaded as gist to github

#I'm shortening the link with my custom shortnet hosted on cloudflare workers

args=${@:1} # Capture user-provided arguments

os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m | tr '[:upper:]' '[:lower:]')
if [ "$arch" = "x86_64" ]; then
    arch="amd64"
fi

repo="bodaay/HuggingFaceModelDownloader"
latest_tag=$(curl --silent "https://api.github.com/repos/$repo/releases/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')

url="https://github.com/${repo}/releases/download/${latest_tag}/hfdownloader_${os}_${arch}_${latest_tag}"

# echo $url
file_path="hfdownloader"

if [[ -f "$file_path" ]]; then
    echo "File already exists. Skipping download."
else
    curl -o $file_path -L "$url" \
        --fail --silent --show-error --output /dev/null &&
        echo "Download successful" ||
        (
            echo "Download failed"
            rm -f $file_path
        )
    chmod +x $file_path
fi

if [[ -z "$args" ]]; then
    args="-h" # Set default arguments if no user-provided arguments
fi

./$file_path "$args"
