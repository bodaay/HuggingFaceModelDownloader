#!/bin/bash

output_dir="output"

echo "Building for arm..."
GOOS=linux GOARCH=arm go build -o "$output_dir/hfdownloader_linux_arm" main.go
echo "Build completed. Binary output: $output_dir/hfdownloader_linux_arm"

echo "Building for arm64..."
GOOS=linux GOARCH=arm64 go build -o "$output_dir/hfdownloader_linux_arm64" main.go
echo "Build completed. Binary output: $output_dir/hfdownloader_linux_arm64"