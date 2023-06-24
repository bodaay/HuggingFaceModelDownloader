#!/bin/bash

output_dir="output"
read -r version < VERSION

echo "Building for Windows/amd64..."
GOOS=windows GOARCH=amd64 go build -o "$output_dir/hfdownloader_windows_amd64_$version.exe" main.go
echo "Build completed. Binary output: $output_dir/hfdownloader_windows_amd64_$version.exe"
