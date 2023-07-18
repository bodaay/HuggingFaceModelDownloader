#!/bin/bash

output_dir="output"
binaryName="hfdownloader"
read -r version < VERSION

echo "Building for darwin/amd64..."
GOOS=darwin GOARCH=amd64 go build -o "$output_dir/${binaryName}_darwin_amd64_$version" main.go
echo "Build completed. Binary output: $output_dir/${binaryName}_darwin_amd64_$version"
