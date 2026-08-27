"""Patterns router — learned patterns browser."""

from fastapi import APIRouter

from sidecar.parsers import parse_learned_patterns
from sidecar.schemas import ListPatternsRequest

router = APIRouter()


@router.post("/list")
def list_patterns(req: ListPatternsRequest):
    path = f"{req.base_path}/.rules/learned-patterns.md"
    patterns = parse_learned_patterns(path)
    return [p.model_dump() for p in patterns]
