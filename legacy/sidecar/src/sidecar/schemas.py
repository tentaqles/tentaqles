"""FastAPI request/response schemas."""

from __future__ import annotations

from pydantic import BaseModel, Field


# --- Scoped (cross-cutting) ---
class ScopedRequest(BaseModel):
    """Base for endpoints that support global / project / local scope."""

    scope: str = "project"  # "global" | "project" | "local"
    workspace_path: str | None = None


class ScopedFileRequest(ScopedRequest):
    """Scoped request targeting a single file by name."""

    filename: str


class ScopedSaveFileRequest(ScopedFileRequest):
    """Scoped request to save a file's content."""

    content: str


class ScopedContentRequest(ScopedRequest):
    """Scoped request carrying text content."""

    content: str


# --- Hierarchy ---
class ScanRequest(BaseModel):
    base_path: str


# --- Config ---
class WorkspaceRequest(BaseModel):
    workspace_path: str


class SaveContentRequest(BaseModel):
    workspace_path: str
    content: str


class RuleContentRequest(BaseModel):
    workspace_path: str
    filename: str


class SaveRuleRequest(BaseModel):
    workspace_path: str
    filename: str
    content: str


class DeleteRuleRequest(BaseModel):
    workspace_path: str
    filename: str


class SaveMcpConfigRequest(BaseModel):
    workspace_path: str
    config: dict
    target: str = ".mcp.json"


class AddMcpServerRequest(BaseModel):
    workspace_path: str
    name: str
    server_config: dict


class RemoveMcpServerRequest(BaseModel):
    workspace_path: str
    name: str


# --- Chat ---
class ChatSendRequest(BaseModel):
    workspace_path: str
    prompt: str
    model: str = "sonnet"
    session_id: str | None = None
    task_id: str | None = None
    permission_mode: str = "default"


class SessionKeyRequest(BaseModel):
    session_key: str


# --- Terminal ---
class TerminalCreateRequest(BaseModel):
    workspace_path: str
    launch_command: str | None = None
    shell: str | None = None
    rows: int = 24
    cols: int = 120


class TerminalDestroyRequest(BaseModel):
    session_id: str


# --- Propagation ---
class PropagationRequest(BaseModel):
    client_path: str
    base_path: str
    config_types: list[str] | None = None


# --- Promotion ---
class PromoteRuleRequest(BaseModel):
    source_path: str
    dest_path: str
    filename: str
    source_level: str = "project"
    dest_level: str = "client"


class PromoteMcpRequest(BaseModel):
    source_path: str
    dest_path: str
    server_name: str
    target_files: list[str] | None = None


class PromoteClaudeMdRequest(BaseModel):
    source_path: str
    dest_path: str


class PromoteAllRequest(BaseModel):
    source_path: str
    dest_path: str
    target_files: list[str] | None = None


# --- Toggle ---
class ToggleRequest(BaseModel):
    workspace_path: str
    name: str
    enabled: bool


class IsEnabledRequest(BaseModel):
    workspace_path: str
    config_type: str
    name: str


# --- Files ---
class FileBrowseRequest(BaseModel):
    workspace_path: str
    query: str = ""
    max_results: int = 50


# --- Templates ---
class PreviewClientRequest(BaseModel):
    base_path: str
    client_name: str
    values: dict[str, str]


class CreateClientRequest(BaseModel):
    base_path: str
    client_name: str
    values: dict[str, str]


# --- Settings ---
class GetSettingsRequest(BaseModel):
    base_path: str


class SaveSettingsRequest(BaseModel):
    base_path: str
    state: dict


# --- Sessions ---
class DiscoverSessionsRequest(BaseModel):
    workspace_path: str
    limit: int = 30


class ListRecentSessionsRequest(BaseModel):
    workspace_path: str
    limit: int = 20


class DeleteSessionRequest(BaseModel):
    workspace_path: str
    session_id: str


# --- Patterns ---
class ListPatternsRequest(BaseModel):
    base_path: str


class SkillsDiscoverRequest(BaseModel):
    workspace_path: str
    client_path: str | None = None


class SkillGetRequest(BaseModel):
    workspace_path: str
    skill_name: str
    client_path: str | None = None


class SkillInstallRequest(BaseModel):
    level: str
    skill_name: str
    source_path: str
    workspace_path: str | None = None
    client_path: str | None = None


class SkillRemoveRequest(BaseModel):
    level: str
    skill_name: str
    workspace_path: str | None = None
    client_path: str | None = None


class SkillPromoteRequest(BaseModel):
    skill_name: str
    from_level: str
    to_level: str
    workspace_path: str
    client_path: str | None = None


class PackInstallRequest(BaseModel):
    pack_name: str
    level: str
    workspace_path: str | None = None
    client_path: str | None = None


# --- Knowledge ---
class KnowledgeDiscoverRequest(BaseModel):
    query: str = ""
    tags: list[str] | None = None
    type_filter: str | None = None
    workspace_path: str | None = None
    client_path: str | None = None


class KnowledgePullRequest(BaseModel):
    id: str
    workspace_path: str | None = None
    client_path: str | None = None


class KnowledgeContributeRequest(BaseModel):
    title: str
    content: str
    tags: list[str] = Field(default_factory=list)
    type: str = "learning"
    workspace_path: str | None = None
    client_path: str | None = None


class KnowledgePromoteRequest(BaseModel):
    id: str
    from_level: str
    to_level: str
    workspace_path: str | None = None
    client_path: str | None = None


class KnowledgeLinkRequest(BaseModel):
    source_id: str
    target_id: str
    workspace_path: str | None = None
    client_path: str | None = None


class KnowledgeGraphRequest(BaseModel):
    workspace_path: str | None = None
    client_path: str | None = None


# --- Personality ---
class PersonalityGetRequest(BaseModel):
    workspace_path: str | None = None
    client_path: str | None = None


class PersonalitySaveRequest(BaseModel):
    file_type: str  # "soul" or "user"
    level: str  # "global", "client", "project"
    content: str
    workspace_path: str | None = None
    client_path: str | None = None


class PersonalityOverridesRequest(BaseModel):
    workspace_path: str | None = None
    client_path: str | None = None


# --- CLAUDE.md Generation ---
class ClaudeMdGenerateRequest(BaseModel):
    workspace_path: str
    client_path: str | None = None


# --- Session Memory ---
class SessionMemoryGetRequest(BaseModel):
    workspace_path: str
    client_path: str | None = None


class SessionMemorySaveRequest(BaseModel):
    workspace_path: str
    section: str
    content: str


class MemoryTimelineRequest(BaseModel):
    workspace_path: str
    client_path: str | None = None
    days: int = 14


# --- Learnings ---
class LearningsGetRequest(BaseModel):
    workspace_path: str
    client_path: str | None = None
    skill_name: str | None = None


class LearningsAppendRequest(BaseModel):
    workspace_path: str
    skill_name: str
    entry: str


class LearningsPromoteRequest(BaseModel):
    workspace_path: str
    client_path: str | None = None
    skill_name: str
    entry_text: str
    to_level: str


# --- Scheduler ---
class JobCreateRequest(BaseModel):
    workspace_path: str
    name: str
    schedule: str
    prompt: str
    model: str = "sonnet"
    enabled: bool = True


class JobUpdateRequest(BaseModel):
    workspace_path: str
    job_id: str
    name: str | None = None
    schedule: str | None = None
    prompt: str | None = None
    model: str | None = None
    enabled: bool | None = None


class JobIdRequest(BaseModel):
    workspace_path: str
    job_id: str


class JobListRequest(BaseModel):
    workspace_path: str


class JobHistoryRequest(BaseModel):
    workspace_path: str
    job_id: str
    limit: int = 20


# --- Brand Context ---
class BrandContextGetRequest(BaseModel):
    workspace_path: str | None = None
    client_path: str | None = None


class BrandContextSaveRequest(BaseModel):
    filename: str
    level: str
    content: str
    workspace_path: str | None = None
    client_path: str | None = None


class CreateProjectRequest(BaseModel):
    client_path: str
    project_name: str
    description: str = ""
    goal: str = ""
    deliverables: str = ""
    tech_stack: str = ""


# --- Boards ---
class BoardListRequest(BaseModel):
    scope: str | None = None  # "global" | "client"
    client_path: str | None = None


class BoardCreateRequest(BaseModel):
    name: str
    scope: str = "client"
    client_path: str | None = None
    columns: list[str] | None = None  # custom column names; None = defaults


class BoardGetRequest(BaseModel):
    board_id: str
    scope: str = "client"
    client_path: str | None = None


class BoardDeleteRequest(BaseModel):
    board_id: str
    scope: str = "client"
    client_path: str | None = None


class CardCreateRequest(BaseModel):
    board_id: str
    title: str
    description: str = ""
    column_id: str | None = None  # None = first column
    priority: str = "medium"
    labels: list[str] = Field(default_factory=list)
    assignee: str | None = None
    due_date: str | None = None
    project_path: str | None = None
    scope: str = "client"
    client_path: str | None = None


class CardUpdateRequest(BaseModel):
    board_id: str
    card_id: str
    title: str | None = None
    description: str | None = None
    priority: str | None = None
    labels: list[str] | None = None
    assignee: str | None = None
    due_date: str | None = None
    project_path: str | None = None
    status: str | None = None
    scope: str = "client"
    client_path: str | None = None


class CardMoveRequest(BaseModel):
    board_id: str
    card_id: str
    target_column_id: str
    position: int = 0
    scope: str = "client"
    client_path: str | None = None


class CardDeleteRequest(BaseModel):
    board_id: str
    card_id: str
    scope: str = "client"
    client_path: str | None = None


class ColumnAddRequest(BaseModel):
    board_id: str
    name: str
    color: str | None = None
    scope: str = "client"
    client_path: str | None = None


class ColumnRemoveRequest(BaseModel):
    board_id: str
    column_id: str
    scope: str = "client"
    client_path: str | None = None


class ColumnReorderRequest(BaseModel):
    board_id: str
    column_ids: list[str]
    scope: str = "client"
    client_path: str | None = None


class CardDelegateRequest(BaseModel):
    board_id: str
    card_id: str
    workspace_path: str  # where to run claude
    model: str = "sonnet"
    scope: str = "client"
    client_path: str | None = None


class CardCancelRequest(BaseModel):
    board_id: str
    card_id: str
    scope: str = "client"
    client_path: str | None = None


class CardCommentCreateRequest(BaseModel):
    board_id: str
    card_id: str
    author: str
    text: str
    parent_id: str | None = None
    scope: str = "client"
    client_path: str | None = None


class CardCommentUpdateRequest(BaseModel):
    board_id: str
    card_id: str
    comment_id: str
    text: str
    scope: str = "client"
    client_path: str | None = None


class CardCommentDeleteRequest(BaseModel):
    board_id: str
    card_id: str
    comment_id: str
    scope: str = "client"
    client_path: str | None = None


class CardCommentReactionRequest(BaseModel):
    board_id: str
    card_id: str
    comment_id: str
    emoji: str
    author: str
    scope: str = "client"
    client_path: str | None = None


class SubCardCreateRequest(BaseModel):
    board_id: str
    parent_card_id: str
    title: str
    priority: str = "medium"
    assignee: str | None = None
    scope: str = "client"
    client_path: str | None = None


class SetParentCardRequest(BaseModel):
    board_id: str
    card_id: str
    parent_card_id: str
    scope: str = "client"
    client_path: str | None = None


class RemoveParentCardRequest(BaseModel):
    board_id: str
    card_id: str
    scope: str = "client"
    client_path: str | None = None


class ReorderSubCardsRequest(BaseModel):
    board_id: str
    parent_card_id: str
    card_ids: list[str]
    scope: str = "client"
    client_path: str | None = None


class RelationshipAddRequest(BaseModel):
    board_id: str
    source_card_id: str
    target_card_id: str
    type: str = "related"
    scope: str = "client"
    client_path: str | None = None


class RelationshipRemoveRequest(BaseModel):
    board_id: str
    relationship_id: str
    scope: str = "client"
    client_path: str | None = None


class ColumnUpdateRequest(BaseModel):
    board_id: str
    column_id: str
    name: str | None = None
    color: str | None = None
    wip_limit: int | None = None
    hidden: bool | None = None
    scope: str = "client"
    client_path: str | None = None


# --- Tasks ---

class TaskListRequest(BaseModel):
    status: str | None = None
    priority: str | None = None
    label: str | None = None
    workspace_path: str | None = None


class TaskGetRequest(BaseModel):
    task_id: str


class TaskCreateRequest(BaseModel):
    title: str
    description: str = ""
    status: str = "backlog"
    priority: str = "medium"
    labels: list[str] = Field(default_factory=list)
    checklist: list[dict] = Field(default_factory=list)
    assignee: str | None = None
    due_date: str | None = None
    workspace_path: str | None = None
    column: str = "backlog"
    position: int = 0
    client_name: str | None = None
    project_path: str | None = None


class TaskUpdateRequest(BaseModel):
    task_id: str
    title: str | None = None
    description: str | None = None
    status: str | None = None
    priority: str | None = None
    labels: list[str] | None = None
    checklist: list[dict] | None = None
    assignee: str | None = None
    due_date: str | None = None
    column: str | None = None
    position: int | None = None
    client_name: str | None = None
    project_path: str | None = None
    execution: dict | None = None


class TaskMoveRequest(BaseModel):
    task_id: str
    column: str
    position: int = 0


class TaskDeleteRequest(BaseModel):
    task_id: str


class TaskCommentRequest(BaseModel):
    task_id: str
    author: str
    text: str


class TaskChecklistToggleRequest(BaseModel):
    task_id: str
    item_index: int


class TaskExecuteRequest(BaseModel):
    task_id: str
    workspace_path: str
    model: str = "sonnet"


class TaskCancelRequest(BaseModel):
    task_id: str


class UnifiedFeedRequest(BaseModel):
    workspace_path: str | None = None
    client_path: str | None = None


class UnifiedCard(BaseModel):
    id: str
    display_id: str
    title: str
    description: str | None = None
    status: str
    priority: str | None = None
    source: str  # "native" | "board" | "gsd" | "sync:<slug>"
    workspace_path: str | None = None
    labels: list[str] = []
    assignee: str | None = None
    # Rich fields
    due_date: str | None = None
    start_date: str | None = None
    completed_at: str | None = None
    created_at: str | None = None
    remote_url: str | None = None
    subtask_count: int = 0
    custom_fields: dict = {}
    sync_provider: str | None = None  # 'asana' | 'github' | etc.
    sync_remote_id: str | None = None  # for push-back
    client_name: str | None = None


# --- Archive ---

class ArchiveProjectRequest(BaseModel):
    project_path: str
    client_path: str


class RestoreProjectRequest(BaseModel):
    project_path: str
    client_path: str


class ListArchivedRequest(BaseModel):
    client_path: str


# --- Diffs ---

class DiffRequest(BaseModel):
    workspace_path: str
    staged: bool = False
    ref: str | None = None


class DiffStatusRequest(BaseModel):
    workspace_path: str
