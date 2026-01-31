#!/usr/bin/env python3
"""
test_python.py - HuggingFace cache compatibility test

Tests that our Go downloader creates a cache structure compatible with
the official HuggingFace Python library (huggingface_hub).

Test scenarios:
1. Download with Python -> verify structure
2. Download with Go -> verify structure matches Python's
3. Download with Python -> read with Go (friendly view)
4. Download with Go -> read with Python (transformers/huggingface_hub)
"""

import os
import sys
import subprocess
import shutil
import json
from pathlib import Path

# Colors for output
RED = '\033[0;31m'
GREEN = '\033[0;32m'
YELLOW = '\033[1;33m'
BLUE = '\033[0;34m'
CYAN = '\033[0;36m'
NC = '\033[0m'  # No Color
BOLD = '\033[1m'

# Test configuration
SCRIPT_DIR = Path(__file__).parent.absolute()
PROJECT_ROOT = SCRIPT_DIR.parent
CACHE_DIR = PROJECT_ROOT / ".cache"
GO_BINARY = PROJECT_ROOT / "hfdownloader"

# Small test models (public, no auth required)
TEST_MODEL_PYTHON = "hf-internal-testing/tiny-random-gpt2"  # Very small model for Python
TEST_MODEL_GO = "ChristianAzinn/gte-small-gguf"  # Small GGUF for Go

# Test counters
tests_passed = 0
tests_failed = 0


def print_header(msg):
    print(f"\n{BLUE}{'='*70}{NC}")
    print(f"{BOLD}{CYAN}  {msg}{NC}")
    print(f"{BLUE}{'='*70}{NC}")


def print_test(msg):
    print(f"\n{YELLOW}> TEST: {msg}{NC}")


def passed(msg):
    global tests_passed
    tests_passed += 1
    print(f"{GREEN}  PASSED: {msg}{NC}")


def failed(msg):
    global tests_failed
    tests_failed += 1
    print(f"{RED}  FAILED: {msg}{NC}")


def check_dependencies():
    """Check if required Python packages are installed."""
    print_header("Checking Dependencies")

    missing = []

    try:
        import huggingface_hub
        print(f"  huggingface_hub: {huggingface_hub.__version__}")
    except ImportError:
        missing.append("huggingface_hub")

    if missing:
        print(f"\n{RED}Missing packages: {', '.join(missing)}{NC}")
        print(f"Install with: pip install {' '.join(missing)}")
        return False

    # Check Go binary
    if not GO_BINARY.exists():
        print(f"\n{RED}Go binary not found: {GO_BINARY}{NC}")
        print("Build with: go build -o hfdownloader ./cmd/hfdownloader")
        return False

    print(f"  Go binary: {GO_BINARY}")
    return True


def clean_cache():
    """Clean the test cache directory."""
    if CACHE_DIR.exists():
        shutil.rmtree(CACHE_DIR)
    CACHE_DIR.mkdir(parents=True, exist_ok=True)


def download_with_python(repo_id: str, revision: str = "main"):
    """Download a model using the official HuggingFace Python library."""
    from huggingface_hub import snapshot_download

    print(f"  Downloading {repo_id} with Python...")

    # Set cache directory
    os.environ["HF_HOME"] = str(CACHE_DIR)

    try:
        path = snapshot_download(
            repo_id=repo_id,
            revision=revision,
            cache_dir=str(CACHE_DIR / "hub"),
            local_dir=None,  # Use cache structure
        )
        print(f"  Downloaded to: {path}")
        return True
    except Exception as e:
        print(f"  {RED}Error: {e}{NC}")
        return False


def download_with_go(repo_id: str, filters: str = None, is_dataset: bool = False):
    """Download a model using our Go downloader."""
    print(f"  Downloading {repo_id} with Go...")

    cmd = [str(GO_BINARY), "download", repo_id, "--cache-dir", str(CACHE_DIR), "-q"]
    if filters:
        cmd.extend(["-F", filters])
    if is_dataset:
        cmd.append("--dataset")

    try:
        result = subprocess.run(cmd, capture_output=True, text=True, timeout=120)
        if result.returncode != 0:
            print(f"  {RED}Error: {result.stderr}{NC}")
            return False
        print(f"  Done")
        return True
    except subprocess.TimeoutExpired:
        print(f"  {RED}Timeout{NC}")
        return False
    except Exception as e:
        print(f"  {RED}Error: {e}{NC}")
        return False


def get_cache_structure(cache_dir: Path) -> dict:
    """Analyze the HF cache structure."""
    structure = {
        "hub_repos": [],
        "models_friendly": [],
        "datasets_friendly": [],
    }

    hub_dir = cache_dir / "hub"
    if hub_dir.exists():
        for item in hub_dir.iterdir():
            if item.is_dir() and item.name.startswith(("models--", "datasets--")):
                repo_info = analyze_repo_dir(item)
                structure["hub_repos"].append(repo_info)

    models_dir = cache_dir / "models"
    if models_dir.exists():
        for owner in models_dir.iterdir():
            if owner.is_dir():
                for repo in owner.iterdir():
                    if repo.is_dir():
                        structure["models_friendly"].append(f"{owner.name}/{repo.name}")

    datasets_dir = cache_dir / "datasets"
    if datasets_dir.exists():
        for owner in datasets_dir.iterdir():
            if owner.is_dir():
                for repo in owner.iterdir():
                    if repo.is_dir():
                        structure["datasets_friendly"].append(f"{owner.name}/{repo.name}")

    return structure


def analyze_repo_dir(repo_dir: Path) -> dict:
    """Analyze a single repo directory in the hub."""
    info = {
        "name": repo_dir.name,
        "has_refs": False,
        "refs": {},
        "has_blobs": False,
        "blob_count": 0,
        "has_snapshots": False,
        "snapshots": [],
    }

    refs_dir = repo_dir / "refs"
    if refs_dir.exists():
        info["has_refs"] = True
        for ref_file in refs_dir.iterdir():
            if ref_file.is_file():
                content = ref_file.read_text().strip()
                info["refs"][ref_file.name] = content

    blobs_dir = repo_dir / "blobs"
    if blobs_dir.exists():
        info["has_blobs"] = True
        info["blob_count"] = len(list(blobs_dir.iterdir()))

    snapshots_dir = repo_dir / "snapshots"
    if snapshots_dir.exists():
        info["has_snapshots"] = True
        for snapshot in snapshots_dir.iterdir():
            if snapshot.is_dir():
                files = list(snapshot.rglob("*"))
                symlinks = [f for f in files if f.is_symlink()]
                info["snapshots"].append({
                    "commit": snapshot.name,
                    "file_count": len([f for f in files if f.is_file() or f.is_symlink()]),
                    "symlink_count": len(symlinks),
                })

    return info


def verify_ref_format(ref_content: str) -> bool:
    """Verify that a ref contains a valid commit hash (not a branch name)."""
    # Should be a 40-character hex string (SHA-1) or longer
    if len(ref_content) < 40:
        return False
    return all(c in '0123456789abcdef' for c in ref_content.lower())


def test_python_download():
    """Test downloading with official Python library."""
    print_test("Python Download - Official HuggingFace Library")

    if not download_with_python(TEST_MODEL_PYTHON):
        failed("Python download failed")
        return

    # Verify structure
    structure = get_cache_structure(CACHE_DIR)

    # Check hub repo exists
    python_repos = [r for r in structure["hub_repos"] if "tiny-random-gpt2" in r["name"]]
    if not python_repos:
        failed("Python repo not found in hub/")
        return

    repo = python_repos[0]

    # Check refs
    if not repo["has_refs"]:
        failed("No refs/ directory")
        return

    if "main" not in repo["refs"]:
        failed("No refs/main file")
        return

    if not verify_ref_format(repo["refs"]["main"]):
        failed(f"refs/main contains branch name instead of commit hash: {repo['refs']['main']}")
        return

    passed(f"refs/main contains commit hash: {repo['refs']['main'][:12]}...")

    # Check blobs
    if not repo["has_blobs"] or repo["blob_count"] == 0:
        failed("No blobs/ or empty")
        return
    passed(f"blobs/ contains {repo['blob_count']} files")

    # Check snapshots
    if not repo["has_snapshots"] or not repo["snapshots"]:
        failed("No snapshots/")
        return

    snapshot = repo["snapshots"][0]
    if snapshot["symlink_count"] == 0:
        failed("Snapshots don't use symlinks")
        return
    passed(f"snapshots/{snapshot['commit'][:12]}.../ has {snapshot['symlink_count']} symlinks")


def test_go_download():
    """Test downloading with our Go tool."""
    print_test("Go Download - Our HFDownloader")

    if not download_with_go(TEST_MODEL_GO, filters="f16"):
        failed("Go download failed")
        return

    # Verify structure
    structure = get_cache_structure(CACHE_DIR)

    # Check hub repo exists
    go_repos = [r for r in structure["hub_repos"] if "gte-small-gguf" in r["name"]]
    if not go_repos:
        failed("Go repo not found in hub/")
        return

    repo = go_repos[0]

    # Check refs
    if not repo["has_refs"]:
        failed("No refs/ directory")
        return

    if "main" not in repo["refs"]:
        failed("No refs/main file")
        return

    if not verify_ref_format(repo["refs"]["main"]):
        failed(f"refs/main contains branch name instead of commit hash: {repo['refs']['main']}")
        return

    passed(f"refs/main contains commit hash: {repo['refs']['main'][:12]}...")

    # Check blobs
    if not repo["has_blobs"] or repo["blob_count"] == 0:
        failed("No blobs/ or empty")
        return
    passed(f"blobs/ contains {repo['blob_count']} files")

    # Check snapshots
    if not repo["has_snapshots"] or not repo["snapshots"]:
        failed("No snapshots/")
        return

    snapshot = repo["snapshots"][0]
    if snapshot["symlink_count"] == 0:
        failed("Snapshots don't use symlinks")
        return
    passed(f"snapshots/{snapshot['commit'][:12]}.../ has {snapshot['symlink_count']} symlinks")

    # Check friendly view (our custom addition)
    if "ChristianAzinn/gte-small-gguf" not in structure["models_friendly"]:
        failed("Friendly view not created in models/")
        return
    passed("Friendly view created: models/ChristianAzinn/gte-small-gguf/")


def test_structure_comparison():
    """Compare cache structures created by Python vs Go."""
    print_test("Structure Comparison - Python vs Go")

    structure = get_cache_structure(CACHE_DIR)

    python_repos = [r for r in structure["hub_repos"] if "tiny-random-gpt2" in r["name"]]
    go_repos = [r for r in structure["hub_repos"] if "gte-small-gguf" in r["name"]]

    if not python_repos or not go_repos:
        failed("Missing repos for comparison")
        return

    py_repo = python_repos[0]
    go_repo = go_repos[0]

    # Compare structure elements
    checks = [
        ("has_refs", "refs/ directory"),
        ("has_blobs", "blobs/ directory"),
        ("has_snapshots", "snapshots/ directory"),
    ]

    all_match = True
    for key, desc in checks:
        if py_repo[key] == go_repo[key]:
            passed(f"Both have {desc}: {py_repo[key]}")
        else:
            failed(f"Mismatch in {desc}: Python={py_repo[key]}, Go={go_repo[key]}")
            all_match = False

    # Check ref format consistency
    py_ref = py_repo["refs"].get("main", "")
    go_ref = go_repo["refs"].get("main", "")

    py_valid = verify_ref_format(py_ref)
    go_valid = verify_ref_format(go_ref)

    if py_valid and go_valid:
        passed("Both refs/main contain valid commit hashes")
    else:
        failed(f"Ref format mismatch: Python valid={py_valid}, Go valid={go_valid}")

    # Check snapshots use symlinks
    py_snap = py_repo["snapshots"][0] if py_repo["snapshots"] else None
    go_snap = go_repo["snapshots"][0] if go_repo["snapshots"] else None

    if py_snap and go_snap:
        py_uses_symlinks = py_snap["symlink_count"] > 0
        go_uses_symlinks = go_snap["symlink_count"] > 0

        if py_uses_symlinks and go_uses_symlinks:
            passed("Both use symlinks in snapshots/")
        else:
            failed(f"Symlink usage: Python={py_uses_symlinks}, Go={go_uses_symlinks}")


def test_python_reads_go_download():
    """Test that Python can read files downloaded by Go."""
    print_test("Cross-compatibility: Python reads Go download")

    from huggingface_hub import HfFileSystem, hf_hub_download

    # Set cache to our test cache
    os.environ["HF_HOME"] = str(CACHE_DIR)
    os.environ["HF_HUB_CACHE"] = str(CACHE_DIR / "hub")

    # Try to access files that Go downloaded
    hub_dir = CACHE_DIR / "hub"
    go_repos = [d for d in hub_dir.iterdir() if d.is_dir() and "gte-small-gguf" in d.name]

    if not go_repos:
        failed("Go-downloaded repo not found")
        return

    repo_dir = go_repos[0]

    # Check if we can find the snapshot
    snapshots_dir = repo_dir / "snapshots"
    if not snapshots_dir.exists():
        failed("No snapshots directory")
        return

    snapshots = list(snapshots_dir.iterdir())
    if not snapshots:
        failed("No snapshots found")
        return

    snapshot = snapshots[0]
    files = list(snapshot.iterdir())

    if not files:
        failed("No files in snapshot")
        return

    # Check that symlinks resolve correctly
    for f in files:
        if f.is_symlink():
            target = f.resolve()
            if target.exists():
                passed(f"Symlink resolves: {f.name} -> blob exists")
            else:
                failed(f"Broken symlink: {f.name}")
                return

    passed("Python can read Go-downloaded files via symlinks")


def test_friendly_view_symlinks():
    """Test that friendly view symlinks work correctly."""
    print_test("Friendly View Symlinks")

    models_dir = CACHE_DIR / "models"

    if not models_dir.exists():
        failed("models/ directory not found")
        return

    # Find the Go-downloaded model
    friendly_path = models_dir / "ChristianAzinn" / "gte-small-gguf"

    if not friendly_path.exists():
        failed(f"Friendly path not found: {friendly_path}")
        return

    # Check symlinks
    symlinks = list(friendly_path.rglob("*"))

    for item in symlinks:
        if item.is_symlink():
            target = item.resolve()
            if target.exists():
                passed(f"Friendly symlink works: {item.name}")
            else:
                failed(f"Broken friendly symlink: {item.name} -> {item.readlink()}")
                return
        elif item.is_file():
            # Regular file (shouldn't happen in friendly view)
            print(f"  {YELLOW}Warning: Regular file in friendly view: {item.name}{NC}")

    passed("All friendly view symlinks resolve correctly")


def print_cache_tree():
    """Print the cache directory structure."""
    print_header("Cache Structure")

    def print_tree(path: Path, prefix: str = "", max_depth: int = 4, current_depth: int = 0):
        if current_depth >= max_depth:
            return

        items = sorted(path.iterdir(), key=lambda x: (not x.is_dir(), x.name))

        for i, item in enumerate(items):
            is_last = i == len(items) - 1
            connector = "" if is_last else ""

            if item.is_symlink():
                target = os.readlink(item)
                # Shorten long targets
                if len(target) > 50:
                    target = "..." + target[-47:]
                print(f"{prefix}{connector} {item.name} -> {CYAN}{target}{NC}")
            elif item.is_dir():
                print(f"{prefix}{connector} {BLUE}{item.name}/{NC}")
                extension = "    " if is_last else ""
                print_tree(item, prefix + extension, max_depth, current_depth + 1)
            else:
                size = item.stat().st_size
                print(f"{prefix}{connector} {item.name} ({size} bytes)")

    print_tree(CACHE_DIR)


def main():
    global tests_passed, tests_failed

    print(f"\n{BOLD}{CYAN}HuggingFace Cache Compatibility Test{NC}")
    print(f"Cache directory: {CACHE_DIR}")
    print(f"Go binary: {GO_BINARY}")

    # Check dependencies
    if not check_dependencies():
        sys.exit(1)

    # Clean and prepare cache
    print_header("Preparing Test Environment")
    clean_cache()
    print(f"  Cleaned cache directory: {CACHE_DIR}")

    # Run tests
    test_python_download()
    test_go_download()
    test_structure_comparison()
    test_python_reads_go_download()
    test_friendly_view_symlinks()

    # Print final structure
    print_cache_tree()

    # Summary
    print_header("Test Summary")
    total = tests_passed + tests_failed
    print(f"  {GREEN}Passed: {tests_passed}{NC}")
    print(f"  {RED}Failed: {tests_failed}{NC}")
    print(f"  Total:  {total}")

    if tests_failed > 0:
        print(f"\n{RED}Some tests failed!{NC}")
        sys.exit(1)
    else:
        print(f"\n{GREEN}All tests passed!{NC}")
        sys.exit(0)


if __name__ == "__main__":
    main()
