#!/usr/bin/env bash

output_dir="output"
binaryName="hfdownloader"
read -r version < VERSION

echo "Building for darwin/arm64..."
GOOS=darwin GOARCH=arm64 go build -o "$output_dir/${binaryName}_darwin_arm64_$version" main.go
echo "Build completed. Binary output: $output_dir/${binaryName}_darwin_arm64_$version"
