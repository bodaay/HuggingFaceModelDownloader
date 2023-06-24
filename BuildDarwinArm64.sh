#!/bin/bash

output_dir="output"
version="1.0.0"

echo "Building for darwin/arm64..."
GOOS=darwin GOARCH=arm64 go build -o "$output_dir/hfdownloader_darwin_arm64_$version" main.go
echo "Build completed. Binary output: $output_dir/hfdownloader_darwin_arm64_$version"
