#!/bin/bash

output_dir="output"
read -r version < VERSION

echo "Building for darwin/amd64..."
GOOS=darwin GOARCH=amd64 go build -o "$output_dir/hfdownloader_darwin_amd64_$version" main.go
echo "Build completed. Binary output: $output_dir/hfdownloader_darwin_amd64_$version"
