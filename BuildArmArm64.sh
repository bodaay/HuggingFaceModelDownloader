#!/usr/bin/env bash

output_dir="output"
binaryName="hfdownloader"
read -r version < VERSION


echo "Building for arm..."
CGO_ENABLED=0 GOOS=linux GOARCH=arm go build -o "$output_dir/${binaryName}_linux_arm_$version" main.go
echo "Build completed. Binary output: $output_dir/${binaryName}_linux_arm_$version"

echo "Building for arm64..."
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o "$output_dir/${binaryName}_linux_arm64_$version" main.go
echo "Build completed. Binary output: $output_dir/${binaryName}_linux_arm64_$version"
