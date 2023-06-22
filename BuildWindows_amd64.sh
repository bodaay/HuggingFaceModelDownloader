#!/bin/bash

output_dir="output/Windows/amd64"

echo "Building for Windows/amd64..."
GOOS=windows GOARCH=amd64 go build -o "$output_dir/hfdownloader.exe" main.go
echo "Build completed. Binary output: $output_dir/hfdownloader.exe"
