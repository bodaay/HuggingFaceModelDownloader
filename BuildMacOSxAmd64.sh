#!/bin/bash

output_dir="output"

echo "Building for MacOSx/amd64..."
GOOS=darwin GOARCH=amd64 go build -o "$output_dir/hfdownloader_macosx_amd64" main.go
echo "Build completed. Binary output: $output_dir/hfdownloader_macosx_amd64"
