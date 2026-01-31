#!/bin/bash
# =============================================================================
# HuggingFace Model Downloader - Interactive Release Script
# =============================================================================
# Automates: version bump, changelog, commit, tag, push, GitHub release
# Uses conventional commits for changelog generation
# =============================================================================

set -e

# Script directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
PURPLE='\033[0;35m'
CYAN='\033[0;36m'
WHITE='\033[1;37m'
NC='\033[0m' # No Color

# Symbols
CHECK="✓"
CROSS="✗"
ARROW="→"
ROCKET="🚀"
TAG="🏷️"
GIT="📦"
TEST="🧪"
BUILD="🔨"

# =============================================================================
# Helper Functions
# =============================================================================

print_header() {
    echo ""
    echo -e "${PURPLE}═══════════════════════════════════════════════════════════════${NC}"
    echo -e "${WHITE}  ${ROCKET} HuggingFace Model Downloader - Release${NC}"
    echo -e "${PURPLE}═══════════════════════════════════════════════════════════════${NC}"
    echo ""
}

print_section() {
    echo ""
    echo -e "${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${WHITE}  $1${NC}"
    echo -e "${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo ""
}

print_success() {
    echo -e "${GREEN}${CHECK} $1${NC}"
}

print_error() {
    echo -e "${RED}${CROSS} $1${NC}"
}

print_warning() {
    echo -e "${YELLOW}⚠ $1${NC}"
}

print_info() {
    echo -e "${BLUE}${ARROW} $1${NC}"
}

prompt_choice() {
    local prompt="$1"
    local default="$2"
    local result

    printf "${YELLOW}%s ${NC}[${WHITE}%s${NC}]: " "$prompt" "$default" >&2
    read -r result
    printf "%s" "${result:-$default}"
}

prompt_yes_no() {
    local prompt="$1"
    local default="$2"
    local result

    if [ "$default" = "y" ]; then
        printf "${YELLOW}%s ${NC}[${WHITE}Y${NC}/n]: " "$prompt"
    else
        printf "${YELLOW}%s ${NC}[y/${WHITE}N${NC}]: " "$prompt"
    fi
    read -r result
    result="${result:-$default}"
    [[ "$result" =~ ^[Yy]$ ]]
}

# =============================================================================
# Check Prerequisites
# =============================================================================

check_prerequisites() {
    print_section "${GIT} Checking Prerequisites"

    local all_good=true

    # Check Go
    if command -v go &> /dev/null; then
        local go_version=$(go version | awk '{print $3}')
        print_success "Go: $go_version"
    else
        print_error "Go not found"
        all_good=false
    fi

    # Check git
    if command -v git &> /dev/null; then
        print_success "Git: $(git --version | awk '{print $3}')"
    else
        print_error "Git not found"
        all_good=false
    fi

    # Check gh CLI
    if command -v gh &> /dev/null; then
        print_success "GitHub CLI: $(gh --version | head -1 | awk '{print $3}')"
        # Check if authenticated
        if gh auth status &> /dev/null; then
            print_success "GitHub CLI: authenticated"
        else
            print_error "GitHub CLI: not authenticated (run: gh auth login)"
            all_good=false
        fi
    else
        print_error "GitHub CLI not found (install: brew install gh)"
        all_good=false
    fi

    # Check we're on a git repo
    if git rev-parse --git-dir &> /dev/null; then
        print_success "Git repository: $(basename $(git rev-parse --show-toplevel))"
    else
        print_error "Not a git repository"
        all_good=false
    fi

    # Check for uncommitted changes
    if git diff-index --quiet HEAD -- 2>/dev/null; then
        print_success "Working tree: clean"
    else
        print_warning "Working tree: has uncommitted changes"
        echo ""
        git status --short
        echo ""
        if ! prompt_yes_no "Continue anyway? (changes will be included in release commit)" "n"; then
            exit 1
        fi
    fi

    # Check VERSION file
    if [ -f "VERSION" ]; then
        CURRENT_VERSION=$(cat VERSION | tr -d '[:space:]')
        print_success "VERSION file: $CURRENT_VERSION"
    else
        print_error "VERSION file not found"
        all_good=false
    fi

    if [ "$all_good" = false ]; then
        print_error "Please fix the above issues before releasing"
        exit 1
    fi
}

# =============================================================================
# Version Management
# =============================================================================

select_version_bump() {
    print_section "${TAG} Version Bump"

    print_info "Current version: ${WHITE}$CURRENT_VERSION${NC}"
    echo ""

    # Parse current version
    IFS='.' read -r major minor patch <<< "$CURRENT_VERSION"

    # Calculate new versions
    local patch_ver="$major.$minor.$((patch + 1))"
    local minor_ver="$major.$((minor + 1)).0"
    local major_ver="$((major + 1)).0.0"

    echo -e "  ${WHITE}1)${NC} Patch  ${GREEN}$patch_ver${NC}  - Bug fixes, minor changes"
    echo -e "  ${WHITE}2)${NC} Minor  ${GREEN}$minor_ver${NC}  - New features, backwards compatible"
    echo -e "  ${WHITE}3)${NC} Major  ${GREEN}$major_ver${NC}  - Breaking changes"
    echo -e "  ${WHITE}4)${NC} Custom          - Enter version manually"
    echo ""

    local choice=$(prompt_choice "Select version bump" "1")

    case $choice in
        1) NEW_VERSION="$patch_ver" ;;
        2) NEW_VERSION="$minor_ver" ;;
        3) NEW_VERSION="$major_ver" ;;
        4)
            echo ""
            NEW_VERSION=$(prompt_choice "Enter new version (without 'v' prefix)" "$patch_ver")
            ;;
        *) NEW_VERSION="$patch_ver" ;;
    esac

    echo ""
    print_success "New version: ${WHITE}$NEW_VERSION${NC}"

    # Validate version format
    if ! [[ "$NEW_VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
        print_error "Invalid version format. Expected: X.Y.Z"
        exit 1
    fi

    # Check if tag already exists
    if git tag -l "v$NEW_VERSION" | grep -q "v$NEW_VERSION"; then
        print_error "Tag v$NEW_VERSION already exists!"
        exit 1
    fi
}

# =============================================================================
# Generate Changelog
# =============================================================================

generate_changelog() {
    print_section "📝 Changelog Generation"

    # Get the last tag
    LAST_TAG=$(git describe --tags --abbrev=0 2>/dev/null || echo "")

    if [ -z "$LAST_TAG" ]; then
        print_info "No previous tags found. Including all commits."
        COMMIT_RANGE="HEAD"
    else
        print_info "Changes since: ${WHITE}$LAST_TAG${NC}"
        COMMIT_RANGE="$LAST_TAG..HEAD"
    fi

    echo ""

    # Collect commits by type (conventional commits)
    local feat_commits=""
    local fix_commits=""
    local docs_commits=""
    local refactor_commits=""
    local other_commits=""

    while IFS= read -r line; do
        if [ -z "$line" ]; then continue; fi

        # Extract commit hash and message
        local hash=$(echo "$line" | cut -d' ' -f1)
        local msg=$(echo "$line" | cut -d' ' -f2-)
        local short_hash="${hash:0:7}"

        # Categorize by conventional commit prefix
        case "$msg" in
            feat:*|feat\(*)
                feat_commits+="- ${msg#feat:} (\`$short_hash\`)"$'\n'
                ;;
            fix:*|fix\(*)
                fix_commits+="- ${msg#fix:} (\`$short_hash\`)"$'\n'
                ;;
            docs:*|docs\(*)
                docs_commits+="- ${msg#docs:} (\`$short_hash\`)"$'\n'
                ;;
            refactor:*|refactor\(*)
                refactor_commits+="- ${msg#refactor:} (\`$short_hash\`)"$'\n'
                ;;
            chore:*|chore\(*|build:*|ci:*|test:*)
                # Skip chore/build/ci/test commits from changelog
                ;;
            *)
                other_commits+="- $msg (\`$short_hash\`)"$'\n'
                ;;
        esac
    done < <(git log --oneline $COMMIT_RANGE 2>/dev/null)

    # Build changelog content
    CHANGELOG_CONTENT="## What's Changed in v$NEW_VERSION"$'\n\n'

    if [ -n "$feat_commits" ]; then
        CHANGELOG_CONTENT+="### Features"$'\n'
        CHANGELOG_CONTENT+="$feat_commits"$'\n'
    fi

    if [ -n "$fix_commits" ]; then
        CHANGELOG_CONTENT+="### Bug Fixes"$'\n'
        CHANGELOG_CONTENT+="$fix_commits"$'\n'
    fi

    if [ -n "$docs_commits" ]; then
        CHANGELOG_CONTENT+="### Documentation"$'\n'
        CHANGELOG_CONTENT+="$docs_commits"$'\n'
    fi

    if [ -n "$refactor_commits" ]; then
        CHANGELOG_CONTENT+="### Refactoring"$'\n'
        CHANGELOG_CONTENT+="$refactor_commits"$'\n'
    fi

    if [ -n "$other_commits" ]; then
        CHANGELOG_CONTENT+="### Other Changes"$'\n'
        CHANGELOG_CONTENT+="$other_commits"$'\n'
    fi

    # Add compare link
    if [ -n "$LAST_TAG" ]; then
        local repo_url=$(git remote get-url origin | sed 's/\.git$//' | sed 's/git@github.com:/https:\/\/github.com\//')
        CHANGELOG_CONTENT+="**Full Changelog**: $repo_url/compare/$LAST_TAG...v$NEW_VERSION"$'\n'
    fi

    # Show preview
    echo -e "${WHITE}Generated changelog:${NC}"
    echo -e "${CYAN}────────────────────────────────────────────────────────────────${NC}"
    echo "$CHANGELOG_CONTENT"
    echo -e "${CYAN}────────────────────────────────────────────────────────────────${NC}"
    echo ""

    if prompt_yes_no "Edit changelog before continuing?" "n"; then
        # Create temp file and open in editor
        local tmpfile=$(mktemp)
        echo "$CHANGELOG_CONTENT" > "$tmpfile"
        ${EDITOR:-vim} "$tmpfile"
        CHANGELOG_CONTENT=$(cat "$tmpfile")
        rm "$tmpfile"
    fi
}

# =============================================================================
# Run Tests
# =============================================================================

run_tests() {
    print_section "${TEST} Running Tests"

    print_info "Running go test..."
    echo ""

    if go test ./... 2>&1; then
        echo ""
        print_success "All tests passed!"
    else
        echo ""
        print_error "Tests failed!"
        if ! prompt_yes_no "Continue anyway?" "n"; then
            exit 1
        fi
    fi
}

# =============================================================================
# Run Local Build
# =============================================================================

run_local_build() {
    print_section "${BUILD} Local Build Test"

    if prompt_yes_no "Run local build test?" "y"; then
        print_info "Building binaries..."
        echo ""

        if ./build.sh; then
            echo ""
            print_success "Build successful!"
        else
            print_error "Build failed!"
            if ! prompt_yes_no "Continue anyway?" "n"; then
                exit 1
            fi
        fi
    else
        print_info "Skipping local build"
    fi
}

# =============================================================================
# Update Version in Files
# =============================================================================

update_version_files() {
    print_section "📄 Updating Version Files"

    # Update VERSION file
    echo "$NEW_VERSION" > VERSION
    print_success "Updated VERSION file"

    # Update version in api.go (server health endpoint)
    local api_file="internal/server/api.go"
    if [ -f "$api_file" ]; then
        if grep -q '"version":' "$api_file"; then
            # Match pattern like: "version": "2.3.3",
            sed -i.bak 's/"version": *"[0-9.]*"/"version": "'"$NEW_VERSION"'"/' "$api_file"
            rm -f "$api_file.bak"
            print_success "Updated version in $api_file"
        fi
    fi

    # Update version in docs if present
    local docs_file="docs/CLI.md"
    if [ -f "$docs_file" ]; then
        # Update any version references in docs
        if grep -q "hfdownloader v[0-9]" "$docs_file"; then
            sed -i.bak "s/hfdownloader v[0-9][0-9.]*[0-9]/hfdownloader v$NEW_VERSION/g" "$docs_file"
            rm -f "$docs_file.bak"
            print_success "Updated version in $docs_file"
        fi
    fi

    # Update README if it has version badge or reference
    if [ -f "README.md" ]; then
        if grep -q "version-[0-9]" "README.md"; then
            sed -i.bak "s/version-[0-9][0-9.]*[0-9]-/version-$NEW_VERSION-/g" "README.md"
            rm -f "README.md.bak"
            print_success "Updated version badge in README.md"
        fi
    fi
}

# =============================================================================
# Git Operations
# =============================================================================

git_commit_and_tag() {
    print_section "${GIT} Git Operations"

    # Stage all changes
    print_info "Staging changes..."
    git add -A

    # Show what will be committed
    echo ""
    echo -e "${WHITE}Changes to be committed:${NC}"
    git status --short
    echo ""

    if ! prompt_yes_no "Commit these changes?" "y"; then
        print_warning "Aborting release"
        exit 1
    fi

    # Commit
    print_info "Creating commit..."
    git commit -m "chore: release v$NEW_VERSION"
    print_success "Created commit"

    # Create annotated tag
    print_info "Creating tag v$NEW_VERSION..."
    git tag -a "v$NEW_VERSION" -m "Release v$NEW_VERSION"$'\n\n'"$CHANGELOG_CONTENT"
    print_success "Created tag v$NEW_VERSION"
}

git_push() {
    print_section "🚀 Push to Remote"

    # Get current branch
    local branch=$(git rev-parse --abbrev-ref HEAD)

    print_info "Current branch: ${WHITE}$branch${NC}"
    print_info "Will push commit and tag to origin"
    echo ""

    if ! prompt_yes_no "Push to origin?" "y"; then
        print_warning "Skipping push. You can push manually with:"
        echo "  git push origin $branch"
        echo "  git push origin v$NEW_VERSION"
        return
    fi

    print_info "Pushing commit..."
    git push origin "$branch"
    print_success "Pushed commit"

    print_info "Pushing tag..."
    git push origin "v$NEW_VERSION"
    print_success "Pushed tag v$NEW_VERSION"
}

# =============================================================================
# Create GitHub Release (Draft)
# =============================================================================

create_github_release() {
    print_section "📦 GitHub Release"

    print_info "Creating draft release on GitHub..."
    echo ""

    # Create draft release
    local release_url=$(gh release create "v$NEW_VERSION" \
        --title "v$NEW_VERSION" \
        --notes "$CHANGELOG_CONTENT" \
        --draft \
        2>&1)

    if [ $? -eq 0 ]; then
        print_success "Draft release created!"
        echo ""
        echo -e "${WHITE}Release URL:${NC}"
        echo -e "  ${CYAN}$release_url${NC}"
        RELEASE_URL="$release_url"
    else
        print_error "Failed to create release: $release_url"
        print_info "You can create it manually at: https://github.com/bodaay/HuggingFaceModelDownloader/releases/new"
    fi
}

# =============================================================================
# Summary and Next Steps
# =============================================================================

show_summary() {
    print_section "✅ Release Summary"

    echo -e "  Version:     ${WHITE}v$NEW_VERSION${NC}"
    echo -e "  Tag:         ${WHITE}v$NEW_VERSION${NC}"
    echo -e "  Status:      ${YELLOW}Draft${NC}"
    echo ""

    if [ -n "$RELEASE_URL" ]; then
        echo -e "${WHITE}Draft Release:${NC}"
        echo -e "  ${CYAN}$RELEASE_URL${NC}"
        echo ""
    fi

    print_section "📋 Next Steps"

    echo -e "${WHITE}To publish the release:${NC}"
    echo ""
    echo -e "  ${WHITE}1.${NC} Go to: ${CYAN}https://github.com/bodaay/HuggingFaceModelDownloader/releases${NC}"
    echo ""
    echo -e "  ${WHITE}2.${NC} Find the draft release ${CYAN}v$NEW_VERSION${NC}"
    echo ""
    echo -e "  ${WHITE}3.${NC} Review the release notes and edit if needed"
    echo ""
    echo -e "  ${WHITE}4.${NC} Click ${GREEN}\"Publish release\"${NC}"
    echo ""
    echo -e "${WHITE}What happens when you publish:${NC}"
    echo -e "  ${ARROW} GitHub Actions workflow triggers automatically"
    echo -e "  ${ARROW} Builds binaries for all platforms (linux, darwin, windows)"
    echo -e "  ${ARROW} Uploads binaries to the release"
    echo -e "  ${ARROW} Generates checksums.txt"
    echo ""

    echo -e "${YELLOW}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${WHITE}  Alternative: Publish from CLI${NC}"
    echo -e "${YELLOW}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo ""
    echo -e "  ${CYAN}gh release edit v$NEW_VERSION --draft=false${NC}"
    echo ""

    # Offer to open in browser
    if command -v open &> /dev/null; then
        if prompt_yes_no "Open release in browser?" "y"; then
            open "https://github.com/bodaay/HuggingFaceModelDownloader/releases"
        fi
    elif command -v xdg-open &> /dev/null; then
        if prompt_yes_no "Open release in browser?" "y"; then
            xdg-open "https://github.com/bodaay/HuggingFaceModelDownloader/releases"
        fi
    fi
}

# =============================================================================
# Main Script
# =============================================================================

main() {
    print_header

    # Run through steps
    check_prerequisites
    select_version_bump
    generate_changelog
    run_tests
    run_local_build
    update_version_files
    git_commit_and_tag
    git_push
    create_github_release
    show_summary

    echo ""
    echo -e "${GREEN}═══════════════════════════════════════════════════════════════${NC}"
    echo -e "${WHITE}  Release v$NEW_VERSION prepared! ${ROCKET}${NC}"
    echo -e "${GREEN}═══════════════════════════════════════════════════════════════${NC}"
    echo ""
}

# Run main function
main "$@"
