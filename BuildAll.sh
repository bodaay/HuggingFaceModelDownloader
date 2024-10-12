#!/usr/bin/env bash

# Path to the main.go file
MAIN_GO="main.go"

# Check if main.go exists
if [[ ! -f "$MAIN_GO" ]]; then
  echo "Error: $MAIN_GO does not exist."
  exit 1
fi

# Extract the version using grep and sed
VERSION=$(grep '^const VERSION' "$MAIN_GO" | sed -E 's/.*= *"([^"]+)".*/\1/')

# Check if VERSION was found
if [[ -z "$VERSION" ]]; then
  echo "Error: VERSION not found in $MAIN_GO."
  exit 1
fi

# Write the version to the VERSION file
echo "$VERSION" > VERSION

echo "Version $VERSION has been written to the VERSION file."

rm output/*
# Build script for all platforms

# Build Windows/amd64
./BuildWindowsAmd64.sh

# Build Linux/amd64
./BuildLinuxAmd64.sh

# Build MacOSx/amd64
./BuildDarwinAmd64.sh

# Build MacOSx/arm64
./BuildDarwinArm64.sh

# Build arm/arm64
./BuildArmArm64.sh

