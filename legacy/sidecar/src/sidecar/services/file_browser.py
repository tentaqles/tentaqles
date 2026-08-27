"""File browser service — lists workspace files for @-mentions and attachments."""

from __future__ import annotations

from pathlib import Path

# Directories to skip during file browsing
_SKIP_DIRS = {
    ".git",
    ".venv",
    "venv",
    "node_modules",
}

# File extensions we care about (code, config, docs)
_CODE_EXTENSIONS = {
    ".py",
    ".js",
    ".ts",
    ".tsx",
    ".jsx",
    ".vue",
    ".svelte",
    ".go",
    ".rs",
    ".java",
    ".kt",
    ".cs",
    ".cpp",
    ".c",
    ".h",
    ".rb",
    ".php",
    ".swift",
    ".scala",
    ".clj",
    ".sql",
    ".graphql",
    ".gql",
    ".html",
    ".css",
    ".scss",
    ".sass",
    ".less",
    ".json",
    ".yaml",
    ".yml",
    ".toml",
    ".ini",
    ".cfg",
    ".conf",
    ".md",
    ".mdx",
    ".rst",
    ".txt",
    ".sh",
    ".bash",
    ".ps1",
    ".psm1",
    ".bat",
    ".cmd",
    ".dockerfile",
    ".tf",
    ".hcl",
    ".env",
    ".gitignore",
    ".editorconfig",
    ".xml",
    ".csv",
    ".ipynb",
    ".r",
    ".rmd",
    ".jl",
    ".tex",
    ".bib",
    ".lock",
    ".prisma",
    ".proto",
    ".pyi",
    ".typed",
    ".jsonc",
    ".jsonl",
    ".ndjson",
    ".log",
    ".tsv",
    ".parquet",
    ".feather",
}

# Max depth to recurse
_MAX_DEPTH = 20


def list_workspace_files(
    workspace_path: str,
    query: str = "",
    max_results: int = 50,
) -> list[dict[str, str]]:
    """List files in a workspace, optionally filtered by a fuzzy query.

    Returns a list of dicts with 'path' (relative) and 'name' keys.
    """
    root = Path(workspace_path)
    if not root.is_dir():
        return []

    files: list[dict[str, str]] = []
    query_lower = query.lower()

    for file_path in _walk_files(root, depth=0):
        rel = file_path.relative_to(root).as_posix()
        name = file_path.name

        # Apply fuzzy filter
        if query_lower:
            if query_lower not in rel.lower() and query_lower not in name.lower():
                continue

        files.append({"path": rel, "name": name, "relative": rel})

    return files


def _walk_files(directory: Path, depth: int) -> list[Path]:
    """Recursively walk directory yielding all files."""
    if depth > _MAX_DEPTH:
        return []

    results: list[Path] = []
    try:
        entries = sorted(directory.iterdir())
    except PermissionError:
        return []

    for entry in entries:
        if entry.is_file():
            results.append(entry)

    for entry in entries:
        if entry.is_dir() and entry.name not in _SKIP_DIRS:
            results.extend(_walk_files(entry, depth + 1))

    return results


# Well-known root files that don't always have extensions
_KNOWN_FILES = {
    "Makefile",
    "Dockerfile",
    "Procfile",
    "Vagrantfile",
    "Rakefile",
    "Gemfile",
    "Pipfile",
    "Taskfile",
    "CLAUDE.md",
    "README.md",
    "CHANGELOG.md",
    "LICENSE",
    "pyproject.toml",
    "setup.py",
    "setup.cfg",
    "package.json",
    "tsconfig.json",
    "webpack.config.js",
    ".gitignore",
    ".env.example",
    ".dockerignore",
}
