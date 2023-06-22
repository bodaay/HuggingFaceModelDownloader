#!/bin/bash

output_dir="output"
version="1.0.0"

echo "Building for Windows/amd64..."
GOOS=windows GOARCH=amd64 go build -o "$output_dir/hfdownloader_windows_amd64_$version.exe" main.go
echo "Build completed. Binary output: $output_dir/hfdownloader_windows_amd64_$version.exe"
