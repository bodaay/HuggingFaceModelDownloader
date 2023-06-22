#!/bin/bash

output_dir="output"

echo "Building for Linux/amd64..."
GOOS=linux GOARCH=amd64 go build -o "$output_dir/hfdownloader_linux_amd64" main.go
echo "Build completed. Binary output: $output_dir/hfdownloader_linux_amd64"
