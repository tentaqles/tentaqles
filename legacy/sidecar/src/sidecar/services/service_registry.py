"""External service registry — tracks API keys, what uses them, and fallbacks."""

from __future__ import annotations

import json
from pathlib import Path

from sidecar.parsers import read_file_content

# Built-in registry of known services
KNOWN_SERVICES = [
    {
        "name": "Firecrawl",
        "key_name": "FIRECRAWL_API_KEY",
        "used_by": ["tool-firecrawl-scraper", "mkt-brand-voice"],
        "what_it_enables": "JS-heavy site scraping, anti-bot bypass, brand asset extraction",
        "without_it": "Falls back to WebFetch (free). If that fails, asks user to paste content.",
        "signup_url": "https://firecrawl.dev",
    },
    {
        "name": "OpenAI",
        "key_name": "OPENAI_API_KEY",
        "used_by": ["str-trending-research"],
        "what_it_enables": "Reddit search via Responses API with real engagement metrics",
        "without_it": "Falls back to WebSearch (free, no engagement metrics)",
        "signup_url": "https://platform.openai.com",
    },
    {
        "name": "YouTube Data API",
        "key_name": "YOUTUBE_API_KEY",
        "used_by": ["tool-youtube"],
        "what_it_enables": "Channel video listing, @handle resolution, search",
        "without_it": "Transcript mode still works with direct URLs (free via yt-dlp)",
        "signup_url": "https://console.cloud.google.com",
    },
    {
        "name": "Google Gemini",
        "key_name": "GEMINI_API_KEY",
        "used_by": ["viz-nano-banana"],
        "what_it_enables": "AI image generation",
        "without_it": "No fallback — requires API key. Free tier available.",
        "signup_url": "https://aistudio.google.com",
    },
]


def get_service_registry(workspace_path: str | None = None) -> list[dict]:
    """Return the service registry.

    Checks for a custom registry at {workspace}/service-registry.json.
    Falls back to built-in registry.
    """
    if workspace_path:
        custom_path = Path(workspace_path) / "service-registry.json"
        content = read_file_content(str(custom_path))
        if content:
            try:
                custom = json.loads(content)
                if isinstance(custom, list):
                    return KNOWN_SERVICES + custom
            except json.JSONDecodeError:
                pass

    return KNOWN_SERVICES


def get_services_for_skill(skill_name: str, workspace_path: str | None = None) -> list[dict]:
    """Get external services required by a specific skill."""
    registry = get_service_registry(workspace_path)
    return [s for s in registry if skill_name in s.get("used_by", [])]
