# Fork Notes

This file documents fork-specific behavior added on top of upstream v3.x.

## Scope

This fork focuses on Web UI storage behavior and cache management across multiple layouts.

## Storage Modes in Web UI (serve)

The server supports three default storage modes for Web UI downloads:

- cache (default)
  - HuggingFace-compatible cache layout with hub plus friendly view
  - Best for Python ecosystem compatibility
- flat
  - Plain files directly under the configured cache root
  - No symlinks
- flat-structured
  - Plain files under <cacheRoot>/<owner>/<repo>/
  - No symlinks

Set default mode at startup:

- hfdownloader serve
- hfdownloader serve --flat
- hfdownloader serve --flat-structured

The Settings page can also update cache directory and default storage mode.

## Analyze and Download Behavior

Analyze and Download actions in the Web UI respect the selected storage mode.
The command preview on Analyze includes storage-mode flags so displayed commands match runtime behavior.

## Cache Page Coverage

The Cache page and Cache API discover repos across all supported layouts:

- HuggingFace cache layout (hub/models--..., hub/datasets--...)
- flat-structured layout (<cacheRoot>/<owner>/<repo>)
- flat-mode indexed downloads via .hfd-flat-index manifests

## Deletion Behavior

Delete from Cache supports all layouts above and includes cleanup for partial/interrupted downloads.

- flat-mode delete removes indexed files and multipart artifacts (.part and .part-*)
- flat-structured and cache-mode delete remove repository directories with safety checks

## Flat-Mode Filename Rules

To reduce root-level collisions in flat mode:

- .gitattributes is skipped
- README.md is renamed to <repo-name>.README.md
- generic root artifacts like mmproj* and imatrix* are prefixed with <repo-name>.

## Compatibility Notes

- Empty top-level cache directories (for example hub after delete) may remain and are generally harmless.
- Existing flat downloads created before flat indexing was introduced may not appear until re-downloaded.
