#!/bin/bash

output_dir="output"
read -r version < VERSION


echo "Building for arm..."
GOOS=linux GOARCH=arm go build -o "$output_dir/hfdownloader_linux_arm_$version" main.go
echo "Build completed. Binary output: $output_dir/hfdownloader_linux_arm_$version"

echo "Building for arm64..."
GOOS=linux GOARCH=arm64 go build -o "$output_dir/hfdownloader_linux_arm64_$version" main.go
echo "Build completed. Binary output: $output_dir/hfdownloader_linux_arm64_$version"
