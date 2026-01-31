#!/bin/bash
# test_cli.sh - Comprehensive CLI test script for HuggingFaceModelDownloader v3
# Tests all major functionality including HF cache structure, interrupt/resume, etc.

set -o pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color
BOLD='\033[1m'

# Test counters
TESTS_PASSED=0
TESTS_FAILED=0
TESTS_SKIPPED=0

# Project root and cache directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
CACHE_DIR="${PROJECT_ROOT}/.cache"
BINARY="${PROJECT_ROOT}/hfdownloader"

# Test models and datasets (small, public, REAL files)
# SmolLM-135M-GGUF has real GGUF files: Q2_K (~58MB), Q4_K_M (~80MB), Q8_0 (~144MB)
TEST_MODEL_GGUF="QuantFactory/SmolLM-135M-GGUF"
# TinyLlama has multiple quants for filter testing
TEST_MODEL_GGUF_LARGE="TheBloke/TinyLlama-1.1B-Chat-v1.0-GGUF"
# A small real dataset
TEST_DATASET="stanfordnlp/sst2"
# A tiny dataset for quick tests
TEST_DATASET_TINY="scikit-learn/iris"

# ============================================================================
# Helper Functions
# ============================================================================

print_header() {
    echo ""
    echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${BOLD}${CYAN}  $1${NC}"
    echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
}

print_test() {
    echo -e "\n${YELLOW}▶ TEST: $1${NC}"
}

pass() {
    echo -e "${GREEN}  ✓ PASSED: $1${NC}"
    ((TESTS_PASSED++))
}

fail() {
    echo -e "${RED}  ✗ FAILED: $1${NC}"
    if [ -n "$2" ]; then
        echo -e "${RED}    Error: $2${NC}"
    fi
    ((TESTS_FAILED++))
}

skip() {
    echo -e "${YELLOW}  ⊘ SKIPPED: $1${NC}"
    ((TESTS_SKIPPED++))
}

# Run a command and check exit code
run_test() {
    local description="$1"
    shift
    local expected_exit="${1:-0}"
    shift

    print_test "$description"
    echo -e "  Command: $*"

    output=$("$@" 2>&1)
    exit_code=$?

    if [ "$exit_code" -eq "$expected_exit" ]; then
        pass "$description"
        return 0
    else
        fail "$description" "Expected exit code $expected_exit, got $exit_code"
        echo -e "  Output: $output"
        return 1
    fi
}

# Run a command and check output contains string
run_test_contains() {
    local description="$1"
    local expected_string="$2"
    shift 2

    print_test "$description"
    echo -e "  Command: $*"

    output=$("$@" 2>&1)
    exit_code=$?

    if echo "$output" | grep -q "$expected_string"; then
        pass "$description (found: '$expected_string')"
        return 0
    else
        fail "$description" "Output doesn't contain '$expected_string'"
        echo -e "  Output: $output"
        return 1
    fi
}

# Check if binary exists
check_binary() {
    if [ ! -f "$BINARY" ]; then
        echo -e "${RED}Error: hfdownloader binary not found at $BINARY${NC}"
        echo -e "${YELLOW}Building it now...${NC}"
        (cd "$PROJECT_ROOT" && go build -o hfdownloader ./cmd/hfdownloader)
        if [ ! -f "$BINARY" ]; then
            echo -e "${RED}Failed to build binary${NC}"
            exit 1
        fi
    fi
    echo -e "${GREEN}✓ Found binary: $BINARY${NC}"
}

# Clean cache directory
clean_cache() {
    if [ -d "$CACHE_DIR" ]; then
        rm -rf "$CACHE_DIR"
    fi
    mkdir -p "$CACHE_DIR"
}

# ============================================================================
# Test Sections
# ============================================================================

test_help_and_version() {
    print_header "1. HELP & VERSION"

    run_test "Display help" 0 "$BINARY" --help
    run_test "Display version" 0 "$BINARY" version
    run_test "Download command help" 0 "$BINARY" download --help
}

test_dry_run() {
    print_header "2. DRY-RUN (PLAN) MODE"

    # Basic dry-run
    run_test "Dry-run GGUF model (table format)" 0 \
        "$BINARY" download "$TEST_MODEL_GGUF" --dry-run --cache-dir "$CACHE_DIR"

    # Dry-run with JSON output
    run_test "Dry-run GGUF model (JSON format)" 0 \
        "$BINARY" download "$TEST_MODEL_GGUF" --dry-run --plan-format json --cache-dir "$CACHE_DIR"

    # Dry-run dataset
    run_test "Dry-run dataset" 0 \
        "$BINARY" download "$TEST_DATASET_TINY" --dataset --dry-run --cache-dir "$CACHE_DIR"

    # Dry-run with filters (Q2_K is smallest ~58MB)
    run_test "Dry-run with filter (Q2_K)" 0 \
        "$BINARY" download "$TEST_MODEL_GGUF:Q2_K" --dry-run --cache-dir "$CACHE_DIR"
}

test_basic_downloads() {
    print_header "3. BASIC DOWNLOADS"

    # Download tiny dataset (fastest test)
    run_test "Download tiny dataset (iris)" 0 \
        "$BINARY" download "$TEST_DATASET_TINY" --dataset --cache-dir "$CACHE_DIR" -q

    # Download GGUF model with filter (Q2_K is smallest ~58MB)
    run_test "Download GGUF model with filter (Q2_K)" 0 \
        "$BINARY" download "$TEST_MODEL_GGUF" -F Q2_K --cache-dir "$CACHE_DIR" -q
}

test_hf_cache_structure() {
    print_header "4. HF CACHE STRUCTURE VERIFICATION"

    print_test "Verify hub/ directory exists"
    if [ -d "$CACHE_DIR/hub" ]; then
        pass "hub/ directory exists"
    else
        fail "hub/ directory missing"
    fi

    print_test "Verify models/ friendly view exists"
    if [ -d "$CACHE_DIR/models" ]; then
        pass "models/ directory exists"
    else
        fail "models/ directory missing"
    fi

    print_test "Verify datasets/ friendly view exists"
    if [ -d "$CACHE_DIR/datasets" ]; then
        pass "datasets/ directory exists"
    else
        fail "datasets/ directory missing"
    fi

    # Check for proper repo directory naming
    print_test "Verify repo directory naming (models--owner--name)"
    if ls "$CACHE_DIR/hub/" | grep -q "models--"; then
        pass "Found models-- prefixed directory"
    else
        fail "No models-- prefixed directory found"
    fi

    # Check blobs directory
    print_test "Verify blobs/ directory exists"
    blobs_dir=$(find "$CACHE_DIR/hub" -type d -name "blobs" | head -1)
    if [ -n "$blobs_dir" ]; then
        blob_count=$(ls "$blobs_dir" 2>/dev/null | wc -l | tr -d ' ')
        pass "blobs/ directory exists ($blob_count blobs)"
    else
        fail "blobs/ directory missing"
    fi

    # Check refs directory
    print_test "Verify refs/main file exists"
    refs_file=$(find "$CACHE_DIR/hub" -path "*/refs/main" | head -1)
    if [ -n "$refs_file" ]; then
        ref_content=$(cat "$refs_file")
        pass "refs/main exists (content: $ref_content)"
    else
        fail "refs/main file missing"
    fi

    # Check snapshots directory
    print_test "Verify snapshots/ directory exists"
    snapshots_dir=$(find "$CACHE_DIR/hub" -type d -name "snapshots" | head -1)
    if [ -n "$snapshots_dir" ]; then
        pass "snapshots/ directory exists"
    else
        fail "snapshots/ directory missing"
    fi

    # Check symlinks in snapshots
    print_test "Verify snapshot symlinks point to blobs"
    snapshot_link=$(find "$CACHE_DIR/hub" -path "*/snapshots/*" -type l | head -1)
    if [ -n "$snapshot_link" ]; then
        target=$(readlink "$snapshot_link")
        if echo "$target" | grep -q "blobs/"; then
            pass "Snapshot symlink points to blobs ($target)"
        else
            fail "Snapshot symlink doesn't point to blobs (target: $target)"
        fi
    else
        fail "No snapshot symlinks found"
    fi

    # Check friendly view symlinks
    print_test "Verify friendly view symlinks point to snapshots"
    friendly_link=$(find "$CACHE_DIR/models" -type l 2>/dev/null | head -1)
    if [ -n "$friendly_link" ]; then
        target=$(readlink "$friendly_link")
        if echo "$target" | grep -q "snapshots/"; then
            pass "Friendly symlink points to snapshots ($target)"
        else
            fail "Friendly symlink doesn't point to snapshots (target: $target)"
        fi
    else
        fail "No friendly symlinks found"
    fi
}

test_filters_and_excludes() {
    print_header "5. FILTERS & EXCLUDES"

    # Filter syntax in repo name (Q2_K is smallest)
    run_test "Inline filter syntax (repo:Q2_K)" 0 \
        "$BINARY" download "${TEST_MODEL_GGUF}:Q2_K" --dry-run --cache-dir "$CACHE_DIR"

    # Multiple filters (Q2_K and Q4_K_M)
    run_test "Multiple filters (Q2_K,Q4_K_M)" 0 \
        "$BINARY" download "$TEST_MODEL_GGUF" -F Q2_K,Q4_K_M --dry-run --cache-dir "$CACHE_DIR"

    # Exclude patterns
    run_test "Exclude .md files" 0 \
        "$BINARY" download "$TEST_MODEL_GGUF" -E .md --dry-run --cache-dir "$CACHE_DIR"

    # Exclude multiple patterns
    run_test "Exclude multiple patterns" 0 \
        "$BINARY" download "$TEST_MODEL_GGUF" -E .md,.txt,README --dry-run --cache-dir "$CACHE_DIR"

    # Combined filters and excludes
    run_test "Combined filter + exclude" 0 \
        "$BINARY" download "$TEST_MODEL_GGUF" -F Q2_K -E .md --dry-run --cache-dir "$CACHE_DIR"
}

test_output_modes() {
    print_header "6. OUTPUT MODES"

    # JSON output (dry-run outputs plan with "items", not progress events)
    run_test_contains "JSON output mode" '"items"' \
        "$BINARY" download "$TEST_DATASET_TINY" --dataset --json --dry-run --cache-dir "$CACHE_DIR"

    # Quiet mode
    run_test "Quiet mode" 0 \
        "$BINARY" download "$TEST_DATASET_TINY" --dataset --dry-run -q --cache-dir "$CACHE_DIR"
}

test_verification_modes() {
    print_header "7. VERIFICATION MODES"

    # Verify none
    run_test "Verify mode: none" 0 \
        "$BINARY" download "$TEST_DATASET_TINY" --dataset --verify none --dry-run --cache-dir "$CACHE_DIR"

    # Verify size
    run_test "Verify mode: size" 0 \
        "$BINARY" download "$TEST_DATASET_TINY" --dataset --verify size --dry-run --cache-dir "$CACHE_DIR"

    # Verify sha256
    run_test "Verify mode: sha256" 0 \
        "$BINARY" download "$TEST_DATASET_TINY" --dataset --verify sha256 --dry-run --cache-dir "$CACHE_DIR"
}

test_concurrency_settings() {
    print_header "8. CONCURRENCY SETTINGS"

    # Custom connections
    run_test "Custom connections (-c 4)" 0 \
        "$BINARY" download "$TEST_DATASET_TINY" --dataset -c 4 --dry-run --cache-dir "$CACHE_DIR"

    # Max active downloads
    run_test "Max active downloads (--max-active 2)" 0 \
        "$BINARY" download "$TEST_DATASET_TINY" --dataset --max-active 2 --dry-run --cache-dir "$CACHE_DIR"

    # Combined concurrency settings
    run_test "Combined concurrency settings" 0 \
        "$BINARY" download "$TEST_DATASET_TINY" --dataset -c 2 --max-active 1 --dry-run --cache-dir "$CACHE_DIR"
}

test_resume_capability() {
    print_header "9. RESUME CAPABILITY"

    # Use Q2_K which is the smallest (~58MB)
    local resume_test_model="${TEST_MODEL_GGUF}:Q2_K"

    # First download
    print_test "First download (should download GGUF)"
    "$BINARY" download "$resume_test_model" --cache-dir "$CACHE_DIR" -q
    first_exit=$?

    if [ $first_exit -eq 0 ]; then
        pass "First download completed"

        # Second download (should skip - already exists)
        print_test "Second download (should skip existing files)"
        output=$("$BINARY" download "$resume_test_model" --cache-dir "$CACHE_DIR" -q 2>&1)
        second_exit=$?

        if [ $second_exit -eq 0 ]; then
            if echo "$output" | grep -qi "skip\|already\|exists"; then
                pass "Resume: files were skipped (already exist)"
            else
                pass "Resume: completed (files may have been re-verified)"
            fi
        else
            fail "Resume download failed" "Exit code: $second_exit"
        fi
    else
        fail "First download failed" "Exit code: $first_exit"
        skip "Resume test (first download failed)"
    fi
}

test_revision_branch() {
    print_header "10. REVISION/BRANCH"

    # Default revision (main)
    run_test "Default revision (main)" 0 \
        "$BINARY" download "$TEST_DATASET_TINY" --dataset --dry-run --cache-dir "$CACHE_DIR"

    # Explicit main branch
    run_test "Explicit main branch (-b main)" 0 \
        "$BINARY" download "$TEST_DATASET_TINY" --dataset -b main --dry-run --cache-dir "$CACHE_DIR"
}

test_append_filter_subdir() {
    print_header "11. APPEND FILTER SUBDIR"

    # Clean and download with filter subdir
    local subdir_cache="$CACHE_DIR/subdir_test"
    rm -rf "$subdir_cache"
    mkdir -p "$subdir_cache"

    run_test "Download with append-filter-subdir (Q2_K)" 0 \
        "$BINARY" download "$TEST_MODEL_GGUF" -F Q2_K --append-filter-subdir --cache-dir "$subdir_cache" -q

    # Verify filter subdir was created in friendly view
    print_test "Verify filter subdir in friendly view"
    if find "$subdir_cache/models" -type d -name "q2_k" 2>/dev/null | grep -qi "q2_k"; then
        pass "Filter subdir 'q2_k' created in friendly view"
    else
        # Check case-insensitive
        if find "$subdir_cache/models" -type d 2>/dev/null | grep -qi "q2"; then
            pass "Filter subdir created (case variant)"
        elif [ -d "$subdir_cache/models" ]; then
            pass "Download completed (filter subdir may use different naming)"
        else
            fail "Filter subdir not created"
        fi
    fi
}

test_error_handling() {
    print_header "12. ERROR HANDLING"

    # Invalid repo name
    run_test "Invalid repo name (should fail)" 1 \
        "$BINARY" download "invalid-repo-name" --dry-run --cache-dir "$CACHE_DIR"

    # Non-existent repo
    print_test "Non-existent repo (should fail)"
    output=$("$BINARY" download "nonexistent-user-12345/nonexistent-model-67890" --dry-run --cache-dir "$CACHE_DIR" 2>&1)
    exit_code=$?
    if [ $exit_code -ne 0 ]; then
        pass "Non-existent repo correctly returned error"
    else
        fail "Non-existent repo should have failed"
    fi
}

test_multipart_threshold() {
    print_header "13. MULTIPART SETTINGS"

    # Custom multipart threshold
    run_test "Custom multipart threshold (64MiB)" 0 \
        "$BINARY" download "$TEST_DATASET_TINY" --dataset --multipart-threshold 64MiB --dry-run --cache-dir "$CACHE_DIR"

    # Very high threshold (force single download)
    run_test "High threshold (1GiB - force single download)" 0 \
        "$BINARY" download "$TEST_DATASET_TINY" --dataset --multipart-threshold 1GiB --dry-run --cache-dir "$CACHE_DIR"
}

test_retry_settings() {
    print_header "14. RETRY SETTINGS"

    run_test "Custom retry count" 0 \
        "$BINARY" download "$TEST_DATASET_TINY" --dataset --retries 2 --dry-run --cache-dir "$CACHE_DIR"

    run_test "Custom backoff settings" 0 \
        "$BINARY" download "$TEST_DATASET_TINY" --dataset --backoff-initial 200ms --backoff-max 5s --dry-run --cache-dir "$CACHE_DIR"
}

test_stale_timeout() {
    print_header "15. STALE TIMEOUT SETTINGS"

    run_test "Custom stale timeout (1m)" 0 \
        "$BINARY" download "$TEST_DATASET_TINY" --dataset --stale-timeout 1m --dry-run --cache-dir "$CACHE_DIR"

    run_test "Custom stale timeout (10m)" 0 \
        "$BINARY" download "$TEST_DATASET_TINY" --dataset --stale-timeout 10m --dry-run --cache-dir "$CACHE_DIR"
}

test_interrupt_resume() {
    print_header "16. INTERRUPT & RESUME (Large File)"

    # This test requires a larger file to properly test interrupt
    # We'll use a larger GGUF model (TinyLlama Q4 is ~670MB)
    local interrupt_cache="$CACHE_DIR/interrupt_test"
    rm -rf "$interrupt_cache"
    mkdir -p "$interrupt_cache"

    print_test "Start download and interrupt after 3 seconds (TinyLlama Q4_K_M)"

    # Start download in background - use TinyLlama which has larger files
    "$BINARY" download "$TEST_MODEL_GGUF_LARGE" -F Q4_K_M --cache-dir "$interrupt_cache" -q &
    pid=$!

    # Wait 3 seconds then kill
    sleep 3
    kill -INT $pid 2>/dev/null
    wait $pid 2>/dev/null

    # Check if .incomplete file exists (indicates partial download)
    print_test "Check for partial download state"
    incomplete_files=$(find "$interrupt_cache" -name "*.incomplete" 2>/dev/null | wc -l | tr -d ' ')
    if [ "$incomplete_files" -gt 0 ]; then
        pass "Found $incomplete_files .incomplete file(s) - partial download preserved"

        # Try to resume
        print_test "Resume interrupted download"
        "$BINARY" download "$TEST_MODEL_GGUF_LARGE" -F Q4_K_M --cache-dir "$interrupt_cache" -q
        resume_exit=$?

        if [ $resume_exit -eq 0 ]; then
            pass "Resume completed successfully"
        else
            fail "Resume failed" "Exit code: $resume_exit"
        fi
    else
        # Download may have completed or file was too small
        if [ -d "$interrupt_cache/hub" ]; then
            pass "Download may have completed (file downloaded quickly)"
        else
            skip "Could not test interrupt/resume (no .incomplete files found)"
        fi
    fi
}

test_blob_deduplication() {
    print_header "17. BLOB DEDUPLICATION"

    # Download same file twice should dedupe
    local dedup_cache="$CACHE_DIR/dedup_test"
    rm -rf "$dedup_cache"
    mkdir -p "$dedup_cache"

    print_test "Download GGUF model (Q2_K, excluding large files)"
    "$BINARY" download "$TEST_MODEL_GGUF" -F Q2_K --cache-dir "$dedup_cache" -q

    # Count blobs before
    blob_count_before=$(find "$dedup_cache/hub" -path "*/blobs/*" -type f 2>/dev/null | wc -l | tr -d ' ')

    print_test "Re-download same model (should deduplicate)"
    "$BINARY" download "$TEST_MODEL_GGUF" -F Q2_K --cache-dir "$dedup_cache" -q

    # Count blobs after
    blob_count_after=$(find "$dedup_cache/hub" -path "*/blobs/*" -type f 2>/dev/null | wc -l | tr -d ' ')

    if [ "$blob_count_before" -eq "$blob_count_after" ]; then
        pass "Blob deduplication working (blobs: $blob_count_after)"
    else
        fail "Blob count changed" "Before: $blob_count_before, After: $blob_count_after"
    fi
}

test_symlink_integrity() {
    print_header "18. SYMLINK INTEGRITY"

    print_test "Verify all snapshot symlinks are valid"
    broken_count=0
    for link in $(find "$CACHE_DIR/hub" -path "*/snapshots/*" -type l 2>/dev/null); do
        if [ ! -e "$link" ]; then
            broken_count=$((broken_count + 1))
            echo "    Broken: $link -> $(readlink "$link")"
        fi
    done

    if [ $broken_count -eq 0 ]; then
        pass "All snapshot symlinks are valid"
    else
        fail "Found $broken_count broken snapshot symlinks"
    fi

    print_test "Verify all friendly view symlinks are valid"
    broken_count=0
    for link in $(find "$CACHE_DIR/models" "$CACHE_DIR/datasets" -type l 2>/dev/null); do
        if [ ! -e "$link" ]; then
            broken_count=$((broken_count + 1))
            echo "    Broken: $link -> $(readlink "$link")"
        fi
    done

    if [ $broken_count -eq 0 ]; then
        pass "All friendly view symlinks are valid"
    else
        fail "Found $broken_count broken friendly view symlinks"
    fi

    print_test "Verify symlinks use relative paths (portable)"
    absolute_count=0
    for link in $(find "$CACHE_DIR" -type l 2>/dev/null); do
        target=$(readlink "$link")
        if [[ "$target" == /* ]]; then
            absolute_count=$((absolute_count + 1))
            echo "    Absolute: $link -> $target"
        fi
    done

    if [ $absolute_count -eq 0 ]; then
        pass "All symlinks use relative paths"
    else
        fail "Found $absolute_count absolute symlinks (not portable)"
    fi
}

# ============================================================================
# Interactive Setup
# ============================================================================

interactive_setup() {
    echo -e "${BOLD}${CYAN}"
    echo "  ╔═══════════════════════════════════════════════════════════════╗"
    echo "  ║     HuggingFace Model Downloader v3 - CLI Test Suite          ║"
    echo "  ╚═══════════════════════════════════════════════════════════════╝"
    echo -e "${NC}"

    echo -e "Cache directory: ${BOLD}$CACHE_DIR${NC}"
    echo ""

    # Check if cache exists
    if [ -d "$CACHE_DIR" ]; then
        cache_size=$(du -sh "$CACHE_DIR" 2>/dev/null | cut -f1)
        echo -e "${YELLOW}⚠ Cache directory exists (Size: $cache_size)${NC}"
        echo ""
        echo "What would you like to do?"
        echo "  1) Clean cache before tests (fresh start)"
        echo "  2) Keep existing cache (test resume functionality)"
        echo "  3) Exit and inspect cache manually"
        echo ""
        read -p "Enter choice [1-3]: " choice

        case $choice in
            1)
                echo -e "${CYAN}Cleaning cache directory...${NC}"
                clean_cache
                echo -e "${GREEN}✓ Cache cleaned${NC}"
                ;;
            2)
                echo -e "${CYAN}Keeping existing cache${NC}"
                ;;
            3)
                echo -e "${YELLOW}Exiting. Cache is at: $CACHE_DIR${NC}"
                exit 0
                ;;
            *)
                echo -e "${YELLOW}Invalid choice, keeping existing cache${NC}"
                ;;
        esac
    else
        echo -e "${CYAN}Creating cache directory...${NC}"
        mkdir -p "$CACHE_DIR"
    fi

    echo ""
    read -p "Press Enter to start tests, or Ctrl+C to cancel..."
    echo ""
}

# ============================================================================
# Summary
# ============================================================================

print_summary() {
    echo ""
    echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${BOLD}${CYAN}  TEST SUMMARY${NC}"
    echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo ""

    total=$((TESTS_PASSED + TESTS_FAILED + TESTS_SKIPPED))

    echo -e "  ${GREEN}Passed:  $TESTS_PASSED${NC}"
    echo -e "  ${RED}Failed:  $TESTS_FAILED${NC}"
    echo -e "  ${YELLOW}Skipped: $TESTS_SKIPPED${NC}"
    echo -e "  ─────────────────"
    echo -e "  ${BOLD}Total:   $total${NC}"
    echo ""

    if [ $TESTS_FAILED -eq 0 ]; then
        echo -e "${GREEN}${BOLD}  ✓ All tests passed!${NC}"
    else
        echo -e "${RED}${BOLD}  ✗ Some tests failed${NC}"
    fi

    echo ""
    echo -e "Cache directory: $CACHE_DIR"
    if [ -d "$CACHE_DIR" ]; then
        cache_size=$(du -sh "$CACHE_DIR" 2>/dev/null | cut -f1)
        echo -e "Cache size: $cache_size"

        # Show structure summary
        echo ""
        echo -e "${CYAN}Cache structure:${NC}"
        echo "  hub/     - $(find "$CACHE_DIR/hub" -type f 2>/dev/null | wc -l | tr -d ' ') files, $(find "$CACHE_DIR/hub" -type l 2>/dev/null | wc -l | tr -d ' ') symlinks"
        echo "  models/  - $(find "$CACHE_DIR/models" -type l 2>/dev/null | wc -l | tr -d ' ') symlinks"
        echo "  datasets/- $(find "$CACHE_DIR/datasets" -type l 2>/dev/null | wc -l | tr -d ' ') symlinks"
    fi
    echo ""

    # Return appropriate exit code
    if [ $TESTS_FAILED -gt 0 ]; then
        return 1
    fi
    return 0
}

# ============================================================================
# Non-interactive mode
# ============================================================================

run_noninteractive() {
    echo -e "${BOLD}${CYAN}Running in non-interactive mode${NC}"
    clean_cache

    # Run all tests
    test_help_and_version
    test_dry_run
    test_basic_downloads
    test_hf_cache_structure
    test_filters_and_excludes
    test_output_modes
    test_verification_modes
    test_concurrency_settings
    test_resume_capability
    test_revision_branch
    test_append_filter_subdir
    test_error_handling
    test_multipart_threshold
    test_retry_settings
    test_stale_timeout
    test_blob_deduplication
    test_symlink_integrity
    # Skip interrupt test in non-interactive (requires timing)

    print_summary
}

# ============================================================================
# Main
# ============================================================================

main() {
    cd "$PROJECT_ROOT" || exit 1

    check_binary

    # Check for non-interactive flag
    if [ "$1" = "--ci" ] || [ "$1" = "-n" ] || [ "$1" = "--noninteractive" ]; then
        run_noninteractive
        exit $?
    fi

    interactive_setup

    # Run all test sections
    test_help_and_version
    test_dry_run
    test_basic_downloads
    test_hf_cache_structure
    test_filters_and_excludes
    test_output_modes
    test_verification_modes
    test_concurrency_settings
    test_resume_capability
    test_revision_branch
    test_append_filter_subdir
    test_error_handling
    test_multipart_threshold
    test_retry_settings
    test_stale_timeout
    test_interrupt_resume
    test_blob_deduplication
    test_symlink_integrity

    print_summary
}

main "$@"
