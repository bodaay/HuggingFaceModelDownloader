#!/bin/bash

output_dir="output/Linux/amd64"

echo "Building for Linux/amd64..."
GOOS=linux GOARCH=amd64 go build -o "$output_dir/hfdownloader" main.go
echo "Build completed. Binary output: $output_dir/hfdownloader"
