#!/bin/bash

output_dir="output/MacOSx/amd64"

echo "Building for MacOSx/amd64..."
GOOS=darwin GOARCH=amd64 go build -o "$output_dir/hfdownloader" main.go
echo "Build completed. Binary output: $output_dir/hfdownloader"
