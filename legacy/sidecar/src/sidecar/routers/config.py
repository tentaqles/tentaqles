"""Configuration CRUD router — rules, MCPs, CLAUDE.md."""

from fastapi import APIRouter

from sidecar.schemas import (
    AddMcpServerRequest,
    DeleteRuleRequest,
    RemoveMcpServerRequest,
    RuleContentRequest,
    SaveContentRequest,
    SaveMcpConfigRequest,
    SaveRuleRequest,
    ScopedRequest,
    WorkspaceRequest,
)
from sidecar.services.config_service import (
    add_mcp_server,
    delete_rule,
    get_claude_md,
    get_mcp_config,
    get_mcp_config_scoped,
    get_mcp_servers,
    get_rule_content,
    get_rules,
    get_rules_with_meta,
    remove_mcp_server,
    save_claude_md,
    save_mcp_config,
    save_mcp_config_scoped,
    save_rule,
)

router = APIRouter()


@router.post("/rules")
def rules_endpoint(req: WorkspaceRequest):
    return get_rules(req.workspace_path)


@router.post("/rules-with-meta")
def rules_meta_endpoint(req: WorkspaceRequest):
    return get_rules_with_meta(req.workspace_path)


@router.post("/claude-md")
def claude_md_endpoint(req: WorkspaceRequest):
    return {"content": get_claude_md(req.workspace_path)}


@router.post("/claude-md/save")
def save_claude_md_endpoint(req: SaveContentRequest):
    save_claude_md(req.workspace_path, req.content)
    return {"ok": True}


@router.post("/rule-content")
def rule_content_endpoint(req: RuleContentRequest):
    return {"content": get_rule_content(req.workspace_path, req.filename)}


@router.post("/rule/save")
def save_rule_endpoint(req: SaveRuleRequest):
    save_rule(req.workspace_path, req.filename, req.content)
    return {"ok": True}


@router.post("/rule/delete")
def delete_rule_endpoint(req: DeleteRuleRequest):
    return {"deleted": delete_rule(req.workspace_path, req.filename)}


@router.post("/mcp-config")
def mcp_config_endpoint(req: WorkspaceRequest):
    return get_mcp_config(req.workspace_path)


@router.post("/mcp-servers")
def mcp_servers_endpoint(req: WorkspaceRequest):
    return get_mcp_servers(req.workspace_path)


@router.post("/mcp-config/save")
def save_mcp_config_endpoint(req: SaveMcpConfigRequest):
    save_mcp_config(req.workspace_path, req.config, req.target)
    return {"ok": True}


@router.post("/mcp-server/add")
def add_mcp_server_endpoint(req: AddMcpServerRequest):
    add_mcp_server(req.workspace_path, req.name, req.server_config)
    return {"ok": True}


@router.post("/mcp-server/remove")
def remove_mcp_server_endpoint(req: RemoveMcpServerRequest):
    remove_mcp_server(req.workspace_path, req.name)
    return {"ok": True}


# --- Scoped MCP endpoints ---


class ScopedMcpSaveRequest(ScopedRequest):
    config: dict


@router.post("/mcp-config-scoped")
def mcp_config_scoped_endpoint(req: ScopedRequest):
    return get_mcp_config_scoped(req.scope, req.workspace_path)


@router.post("/mcp-config-scoped/save")
def save_mcp_config_scoped_endpoint(req: ScopedMcpSaveRequest):
    save_mcp_config_scoped(req.scope, req.config, req.workspace_path)
    return {"ok": True}
