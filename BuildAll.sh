#!/bin/bash

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
