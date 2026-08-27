"""Promotion router — push configs upstream (project → client)."""

import dataclasses

from fastapi import APIRouter

from sidecar.schemas import (
    PromoteAllRequest,
    PromoteClaudeMdRequest,
    PromoteMcpRequest,
    PromoteRuleRequest,
)
from sidecar.services.promotion import (
    preview_promote_claude_md,
    preview_promote_mcp,
    preview_promote_rule,
    promote_all_mcps,
    promote_all_rules,
    promote_claude_md,
    promote_mcp,
    promote_rule,
)

router = APIRouter()


@router.post("/preview/rule")
def preview_rule(req: PromoteRuleRequest):
    result = preview_promote_rule(req.source_path, req.dest_path, req.filename, req.source_level, req.dest_level)
    return dataclasses.asdict(result)


@router.post("/preview/mcp")
def preview_mcp_endpoint(req: PromoteMcpRequest):
    result = preview_promote_mcp(req.source_path, req.dest_path, req.server_name)
    return dataclasses.asdict(result)


@router.post("/preview/claude-md")
def preview_claude_md_endpoint(req: PromoteClaudeMdRequest):
    result = preview_promote_claude_md(req.source_path, req.dest_path)
    return dataclasses.asdict(result)


@router.post("/execute/rule")
def execute_rule(req: PromoteRuleRequest):
    return {"success": promote_rule(req.source_path, req.dest_path, req.filename)}


@router.post("/execute/mcp")
def execute_mcp_endpoint(req: PromoteMcpRequest):
    return {"success": promote_mcp(req.source_path, req.dest_path, req.server_name, req.target_files)}


@router.post("/execute/claude-md")
def execute_claude_md_endpoint(req: PromoteClaudeMdRequest):
    return {"success": promote_claude_md(req.source_path, req.dest_path)}


@router.post("/execute/all-rules")
def execute_all_rules(req: PromoteAllRequest):
    return {"promoted": promote_all_rules(req.source_path, req.dest_path)}


@router.post("/execute/all-mcps")
def execute_all_mcps(req: PromoteAllRequest):
    return {"promoted": promote_all_mcps(req.source_path, req.dest_path, req.target_files)}
