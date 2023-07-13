#!/bin/bash

output_dir="output"
read -r version < VERSION

echo "Building for darwin/arm64..."
GOOS=darwin GOARCH=arm64 go build -o "$output_dir/hfdownloader_darwin_arm64_$version" main.go
echo "Build completed. Binary output: $output_dir/hfdownloader_darwin_arm64_$version"
