// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package hfdownloader

import (
	"fmt"
	"os"
	"path/filepath"
)

// RebuildScriptVersion is the current version of the embedded shell script.
const RebuildScriptVersion = "3.0.0"

// RebuildScript is the shell script content for rebuilding friendly view.
// This can be run standalone without the Go binary.
const RebuildScript = `#!/bin/bash
# HFDownloader Rebuild Script v%s
# Regenerates the friendly view (models/, datasets/) from the hub cache
# Auto-generated - manual edits will be overwritten
#
# Usage: ./rebuild.sh [--clean] [--verbose]
#
# This script creates human-readable symlinks:
#   models/{owner}/{repo}/{file} -> hub/models--{owner}--{repo}/snapshots/{commit}/{file}
#   datasets/{owner}/{repo}/{file} -> hub/datasets--{owner}--{repo}/snapshots/{commit}/{file}

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CLEAN=false
VERBOSE=false

# Parse arguments
for arg in "$@"; do
    case $arg in
        --clean)
            CLEAN=true
            ;;
        --verbose|-v)
            VERBOSE=true
            ;;
        --help|-h)
            echo "Usage: $0 [--clean] [--verbose]"
            echo ""
            echo "Regenerates the friendly view symlinks from the hub cache."
            echo ""
            echo "Options:"
            echo "  --clean     Remove orphaned (broken) symlinks"
            echo "  --verbose   Print detailed progress"
            echo "  --help      Show this help"
            exit 0
            ;;
    esac
done

log() {
    if [ "$VERBOSE" = true ]; then
        echo "$@"
    fi
}

# Statistics
REPOS_SCANNED=0
SYMLINKS_CREATED=0
SYMLINKS_UPDATED=0
ORPHANS_REMOVED=0

# Process a single repo directory
process_repo() {
    local repo_dir="$1"
    local repo_name="$(basename "$repo_dir")"

    # Parse repo type and name from directory name (e.g., models--owner--name)
    local repo_type=""
    local rest=""

    if [[ "$repo_name" == models--* ]]; then
        repo_type="models"
        rest="${repo_name#models--}"
    elif [[ "$repo_name" == datasets--* ]]; then
        repo_type="datasets"
        rest="${repo_name#datasets--}"
    else
        return 0  # Not a valid repo directory
    fi

    # Split owner--name
    local owner="${rest%%--*}"
    local name="${rest#*--}"

    if [ "$owner" = "$rest" ] || [ -z "$name" ]; then
        return 0  # Invalid format
    fi

    REPOS_SCANNED=$((REPOS_SCANNED + 1))
    log "Processing: $repo_type/$owner/$name"

    # Find current commit from refs
    local commit=""
    for ref in main master; do
        local ref_file="$repo_dir/refs/$ref"
        if [ -f "$ref_file" ]; then
            commit="$(cat "$ref_file" | tr -d '[:space:]')"
            if [ -n "$commit" ]; then
                break
            fi
        fi
    done

    # If no refs found, try first snapshot
    if [ -z "$commit" ] && [ -d "$repo_dir/snapshots" ]; then
        commit="$(ls -1 "$repo_dir/snapshots" 2>/dev/null | head -1)"
    fi

    if [ -z "$commit" ]; then
        log "  No snapshots found, skipping"
        return 0
    fi

    local snapshot_dir="$repo_dir/snapshots/$commit"
    if [ ! -d "$snapshot_dir" ]; then
        log "  Snapshot directory missing: $commit"
        return 0
    fi

    # Create friendly directory
    local friendly_dir="$SCRIPT_DIR/$repo_type/$owner/$name"
    mkdir -p "$friendly_dir"

    # Create symlinks for all files in snapshot
    while IFS= read -r -d '' file; do
        local rel_path="${file#$snapshot_dir/}"
        local friendly_path="$friendly_dir/$rel_path"
        local friendly_parent="$(dirname "$friendly_path")"

        # Ensure parent directory exists
        mkdir -p "$friendly_parent"

        # Calculate relative path from friendly location to snapshot
        local target_path="$file"
        local rel_target="$(python3 -c "import os.path; print(os.path.relpath('$target_path', '$friendly_parent'))" 2>/dev/null || realpath --relative-to="$friendly_parent" "$target_path" 2>/dev/null)"

        if [ -z "$rel_target" ]; then
            # Fallback: construct relative path manually
            local depth=$(echo "$friendly_path" | tr -cd '/' | wc -c)
            local prefix=""
            for ((i=0; i<depth-2; i++)); do
                prefix="../$prefix"
            done
            rel_target="${prefix}hub/$repo_name/snapshots/$commit/$rel_path"
        fi

        # Check if symlink already exists and is correct
        if [ -L "$friendly_path" ]; then
            local existing_target="$(readlink "$friendly_path")"
            if [ "$existing_target" = "$rel_target" ]; then
                continue  # Already correct
            fi
            rm "$friendly_path"
            SYMLINKS_UPDATED=$((SYMLINKS_UPDATED + 1))
        else
            SYMLINKS_CREATED=$((SYMLINKS_CREATED + 1))
        fi

        # Create symlink
        ln -s "$rel_target" "$friendly_path"
        log "  Created: $rel_path"

    done < <(find "$snapshot_dir" -type f -print0 -o -type l -print0)
}

# Clean orphaned symlinks in a directory
clean_orphans() {
    local dir="$1"

    if [ ! -d "$dir" ]; then
        return 0
    fi

    while IFS= read -r -d '' link; do
        if [ -L "$link" ] && [ ! -e "$link" ]; then
            rm "$link"
            ORPHANS_REMOVED=$((ORPHANS_REMOVED + 1))
            log "  Removed orphan: $link"
        fi
    done < <(find "$dir" -type l -print0)

    # Clean empty directories
    find "$dir" -type d -empty -delete 2>/dev/null || true
}

# Main execution
echo "Rebuilding friendly view from: $SCRIPT_DIR"

# Check hub directory exists
if [ ! -d "$SCRIPT_DIR/hub" ]; then
    echo "No hub directory found. Nothing to rebuild."
    exit 0
fi

# Process all repos in hub/
for repo_dir in "$SCRIPT_DIR/hub"/*; do
    if [ -d "$repo_dir" ]; then
        process_repo "$repo_dir"
    fi
done

# Clean orphaned symlinks if requested
if [ "$CLEAN" = true ]; then
    echo "Cleaning orphaned symlinks..."
    clean_orphans "$SCRIPT_DIR/models"
    clean_orphans "$SCRIPT_DIR/datasets"
fi

# Print summary
echo ""
echo "Rebuild complete:"
echo "  Repos scanned:     $REPOS_SCANNED"
echo "  Symlinks created:  $SYMLINKS_CREATED"
echo "  Symlinks updated:  $SYMLINKS_UPDATED"
if [ "$CLEAN" = true ]; then
    echo "  Orphans removed:   $ORPHANS_REMOVED"
fi
`

// WriteRebuildScript writes the rebuild shell script to the cache directory.
// Returns the path to the written script and any error.
func (c *HFCache) WriteRebuildScript() (string, error) {
	scriptPath := filepath.Join(c.Root, "rebuild.sh")

	content := fmt.Sprintf(RebuildScript, RebuildScriptVersion)

	// Check if script already exists and is current version
	if existing, err := os.ReadFile(scriptPath); err == nil {
		expectedHeader := fmt.Sprintf("# HFDownloader Rebuild Script v%s", RebuildScriptVersion)
		if len(existing) > 100 && string(existing[:len(expectedHeader)+50]) == content[:len(expectedHeader)+50] {
			// Script exists and is current version
			return scriptPath, nil
		}
	}

	// Write the script
	if err := os.WriteFile(scriptPath, []byte(content), 0o755); err != nil {
		return "", fmt.Errorf("write rebuild script: %w", err)
	}

	return scriptPath, nil
}

// RebuildScriptPath returns the expected path of the rebuild script.
func (c *HFCache) RebuildScriptPath() string {
	return filepath.Join(c.Root, "rebuild.sh")
}
