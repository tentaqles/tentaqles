"""Brand context service — voice profile, positioning, ICP, samples, assets."""

from __future__ import annotations

from pathlib import Path

from sidecar.models import BrandContext, BrandContextFile
from sidecar.parsers import read_file_content

BRAND_FILES = ["voice-profile.md", "positioning.md", "icp.md", "samples.md", "assets.md"]
BRAND_FIELD_MAP = {
    "voice-profile.md": "voice_profile",
    "positioning.md": "positioning",
    "icp.md": "icp",
    "samples.md": "samples",
    "assets.md": "assets",
}


def _brand_dir(level: str, workspace_path: str | None, client_path: str | None) -> Path:
    if level == "global":
        return Path.home() / ".tentaqles" / "brand_context"
    elif level == "client" and client_path:
        return Path(client_path) / "brand_context"
    elif level == "project" and workspace_path:
        return Path(workspace_path) / "brand_context"
    raise ValueError(f"Cannot resolve brand_context dir for level={level}")


def _read_brand_file(
    filename: str, level: str, workspace_path: str | None, client_path: str | None
) -> BrandContextFile:
    try:
        path = _brand_dir(level, workspace_path, client_path) / filename
    except ValueError:
        return BrandContextFile(filename=filename, level=level, exists=False)

    content = read_file_content(str(path))
    return BrandContextFile(
        filename=filename,
        level=level,
        file_path=str(path),
        exists=content is not None,
        content=content or "",
    )


def get_brand_context(workspace_path: str | None = None, client_path: str | None = None) -> BrandContext:
    """Resolve brand context with hierarchy: project > client > global."""
    context = BrandContext()

    for filename in BRAND_FILES:
        field_name = BRAND_FIELD_MAP[filename]
        # Check levels in order: project > client > global
        for level in ["project", "client", "global"]:
            if level == "project" and not workspace_path:
                continue
            if level == "client" and not client_path:
                continue

            bf = _read_brand_file(filename, level, workspace_path, client_path)
            if bf.exists:
                setattr(context, field_name, bf)
                context.effective_level = level
                break
        else:
            # No file found at any level
            setattr(context, field_name, BrandContextFile(filename=filename, level="none", exists=False))

    return context


def save_brand_context(
    filename: str,
    level: str,
    content: str,
    workspace_path: str | None = None,
    client_path: str | None = None,
) -> str:
    """Save a brand context file at the specified level."""
    if filename not in BRAND_FILES:
        return f"Invalid filename: {filename}. Must be one of: {', '.join(BRAND_FILES)}"

    brand_dir = _brand_dir(level, workspace_path, client_path)
    brand_dir.mkdir(parents=True, exist_ok=True)
    path = brand_dir / filename
    path.write_text(content, encoding="utf-8")
    return str(path)


def list_brand_overrides(workspace_path: str | None = None, client_path: str | None = None) -> dict:
    """Return all brand context files across levels."""
    result = {}
    for filename in BRAND_FILES:
        field_name = BRAND_FIELD_MAP[filename]
        result[field_name] = {}
        for level in ["global", "client", "project"]:
            if level == "project" and not workspace_path:
                continue
            if level == "client" and not client_path:
                continue
            bf = _read_brand_file(filename, level, workspace_path, client_path)
            result[field_name][level] = bf.model_dump()
    return result
