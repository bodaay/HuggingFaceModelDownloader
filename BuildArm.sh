#!/bin/bash

output_dir_arm="output/Linux/arm"
output_dir_arm64="output/Linux/arm64"

echo "Building for arm..."
GOOS=linux GOARCH=arm go build -o "$output_dir_arm/hfdownloader" main.go
echo "Build completed. Binary output: $output_dir_arm/hfdownloader"

echo "Building for arm64..."
GOOS=linux GOARCH=arm64 go build -o "$output_dir_arm64/hfdownloader" main.go
echo "Build completed. Binary output: $output_dir_arm64/hfdownloader"
