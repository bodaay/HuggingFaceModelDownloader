#!/bin/bash

output_dir="output"
version="1.0.0"

echo "Building for Linux/amd64..."
GOOS=linux GOARCH=amd64 go build -o "$output_dir/hfdownloader_linux_amd64_$version" main.go
echo "Build completed. Binary output: $output_dir/hfdownloader_linux_amd64_$version"
