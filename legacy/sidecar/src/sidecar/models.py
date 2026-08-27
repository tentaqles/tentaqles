"""Pydantic data models for workspace configuration."""

from __future__ import annotations

from enum import Enum

from pydantic import BaseModel, Field


class GitPlatform(str, Enum):
    GITHUB = "github"
    GITLAB = "gitlab"
    AZURE_DEVOPS = "azure-devops"


class CloudProvider(str, Enum):
    AZURE = "azure"
    GCP = "gcp"
    NONE = "none"


class DatabaseType(str, Enum):
    SNOWFLAKE = "snowflake"
    POSTGRESQL = "postgresql"
    DATABRICKS = "databricks"
    VARIES = "varies"
    NONE = "none"


class Language(str, Enum):
    EN = "en"
    PT_BR = "pt-br"


class GitIdentity(BaseModel):
    name: str
    email: str
    platform: GitPlatform
    host: str
    account: str | None = None
    hooks_path: str | None = None


class CloudIdentity(BaseModel):
    provider: CloudProvider
    subscription_name: str | None = None
    subscription_id: str | None = None


class DatabaseConfig(BaseModel):
    type: DatabaseType
    dialect: str | None = None
    connection_details: dict | None = None


class DevelopmentRules(BaseModel):
    blocked_commands: list[str] = Field(default_factory=list)
    confirm_commands: list[str] = Field(default_factory=list)
    allowed_commands: list[str] = Field(default_factory=list)


class WorkspaceConfig(BaseModel):
    client_name: str
    client_description: str
    workspace_path: str
    git: GitIdentity
    cloud: CloudIdentity
    database: DatabaseConfig
    language: Language = Language.EN
    tech_stack: list[str] = Field(default_factory=list)
    special_rules: list[str] = Field(default_factory=list)
    active_mcp_servers: list[str] = Field(default_factory=list)
    sql_dialect_rules: str | None = None
    identity_rules: str | None = None
    workspace_rules: dict[str, str] = Field(default_factory=dict)
    claude_md_content: str | None = None
    development_rules: DevelopmentRules | None = None


class VerificationResult(BaseModel):
    check: str
    passed: bool
    expected: str
    actual: str
    command: str


class IdentityVerification(BaseModel):
    git_email: VerificationResult
    azure_subscription: VerificationResult | None = None
    github_account: VerificationResult | None = None


class LearnedPattern(BaseModel):
    number: int
    title: str
    category: str
    date_discovered: str
    description: str
    wrong_example: str
    right_example: str
    prevention: str | None = None
    applied_to: list[str] = Field(default_factory=list)


class SkillLevel(str, Enum):
    GLOBAL = "global"
    CLIENT = "client"
    PROJECT = "project"


class SkillMeta(BaseModel):
    """Metadata for a discovered skill."""

    name: str
    description: str | None = None
    version: str | None = None
    category: str | None = None
    level: SkillLevel = SkillLevel.GLOBAL
    source_path: str = ""
    enabled: bool = True
    body: str = ""
    files: list[str] = Field(default_factory=list)
    context_needs: dict[str, str] = Field(default_factory=dict)
    dependencies: list[str] = Field(default_factory=list)


class SkillPack(BaseModel):
    """A bundled collection of related skills."""

    name: str
    version: str = "1.0.0"
    description: str = ""
    skills: list[str] = Field(default_factory=list)
    dependencies: list[str] = Field(default_factory=list)
    category_prefix: str = ""


class KnowledgeType(str, Enum):
    LEARNING = "learning"
    DECISION = "decision"
    PATTERN = "pattern"
    SOLUTION = "solution"


class KnowledgeLevel(str, Enum):
    GLOBAL = "global"
    CLIENT = "client"
    PROJECT = "project"


class KnowledgeEntry(BaseModel):
    """A knowledge graph entry."""

    id: str = ""
    title: str = ""
    tags: list[str] = Field(default_factory=list)
    source_project: str | None = None
    source_client: str | None = None
    author: str | None = None
    date: str = ""
    linked: list[str] = Field(default_factory=list)
    type: KnowledgeType = KnowledgeType.LEARNING
    level: KnowledgeLevel = KnowledgeLevel.PROJECT
    file_path: str = ""
    body: str = ""


class PersonalityFile(BaseModel):
    """One personality file at a specific level."""

    level: str = ""
    file_path: str = ""
    exists: bool = False
    content: str = ""


class PersonalityPair(BaseModel):
    """Resolved soul + user personality."""

    soul: PersonalityFile = Field(default_factory=PersonalityFile)
    user: PersonalityFile = Field(default_factory=PersonalityFile)
    effective_level: str = "global"


class WorkspaceSummary(BaseModel):
    client_name: str
    workspace_path: str
    git_email: str
    git_platform: str
    cloud_provider: str
    database_type: str
    language: str
    special_rules: list[str] = Field(default_factory=list)


# --- Models for tentaqles (web app) ---


class GitProfileRef(BaseModel):
    """Git platform reference in .workspace-profile.json (no name/email — those come from .gitconfig)."""

    platform: GitPlatform
    host: str = "github.com"
    account: str | None = None


class CloudProfileRef(BaseModel):
    """Cloud provider reference in .workspace-profile.json."""

    provider: CloudProvider
    subscription_name: str | None = None
    subscription_id: str | None = None


class DatabaseProfileRef(BaseModel):
    """Database reference in .workspace-profile.json."""

    type: DatabaseType
    dialect: str | None = None
    connection_details: dict | None = None


class WorkspaceProfile(BaseModel):
    """File-based client profile stored as .workspace-profile.json.

    Replaces the hardcoded CLIENT_PROFILES dict in config.py.
    Git identity (name, email) is still read from .gitconfig-{client}.
    """

    schema_version: str = Field(default="workspace-profile-v1", alias="$schema")
    client_name: str
    client_description: str
    git: GitProfileRef
    cloud: CloudProfileRef
    database: DatabaseProfileRef
    language: Language = Language.EN
    tech_stack: list[str] = Field(default_factory=list)
    special_rules: list[str] = Field(default_factory=list)

    model_config = {"populate_by_name": True}


class ToggleState(BaseModel):
    """Toggle state for config items at a given hierarchy level."""

    rules: dict[str, bool] = Field(default_factory=dict)
    mcps: dict[str, bool] = Field(default_factory=dict)
    hooks: dict[str, bool] = Field(default_factory=dict)
    skills: dict[str, bool] = Field(default_factory=dict)
    commands: dict[str, bool] = Field(default_factory=dict)


class ManagerState(BaseModel):
    """Per-client management state stored as .tentaqles.json."""

    schema_version: str = Field(default="tentaqles-v1", alias="$schema")
    toggles: ToggleState = Field(default_factory=ToggleState)
    propagation_excludes: list[str] = Field(default_factory=list)
    last_propagated_at: str | None = None

    model_config = {"populate_by_name": True}


class ManagerSettings(BaseModel):
    """Application settings."""

    auto_propagate_on_save: bool = False
    terminal_shell: str = "powershell"
    terminal_claude_args: list[str] = Field(default_factory=list)
    ui_port: int = 8080


class GlobalManagerState(BaseModel):
    """Global management state stored at base folder .tentaqles.json."""

    schema_version: str = Field(default="tentaqles-global-v1", alias="$schema")
    base_path: str = ""
    global_toggles: ToggleState = Field(default_factory=ToggleState)
    settings: ManagerSettings = Field(default_factory=ManagerSettings)

    model_config = {"populate_by_name": True}


class SessionBlock(BaseModel):
    """A single session block from a daily memory file."""

    session_number: int = 1
    goal: str = ""
    deliverables: list[str] = Field(default_factory=list)
    decisions: list[str] = Field(default_factory=list)
    open_threads: list[str] = Field(default_factory=list)
    project: str | None = None
    raw_content: str = ""


class DailyMemory(BaseModel):
    """A single day's memory file."""

    date: str = ""
    level: str = ""
    file_path: str = ""
    sessions: list[SessionBlock] = Field(default_factory=list)
    raw_content: str = ""


class ScheduledJob(BaseModel):
    """A scheduled job definition."""

    id: str = ""
    name: str = ""
    schedule: str = ""
    workspace: str = ""
    prompt: str = ""
    model: str = "sonnet"
    enabled: bool = True
    created: str = ""
    last_run: str | None = None
    last_status: str | None = None


class JobRunResult(BaseModel):
    """Result of a single job execution."""

    job_id: str = ""
    timestamp: str = ""
    status: str = ""
    output: str = ""
    error: str | None = None
    duration_ms: float = 0.0
    cost_usd: float = 0.0
    input_tokens: int = 0
    output_tokens: int = 0


class BrandContextFile(BaseModel):
    """A single brand context file."""

    filename: str = ""
    level: str = ""
    file_path: str = ""
    exists: bool = False
    content: str = ""


class BrandContext(BaseModel):
    """Full brand context for a workspace."""

    voice_profile: BrandContextFile = Field(default_factory=BrandContextFile)
    positioning: BrandContextFile = Field(default_factory=BrandContextFile)
    icp: BrandContextFile = Field(default_factory=BrandContextFile)
    samples: BrandContextFile = Field(default_factory=BrandContextFile)
    assets: BrandContextFile = Field(default_factory=BrandContextFile)
    effective_level: str = "global"


# --- Activation & Health ---


class ClaudeProfile(BaseModel):
    """Per-client Claude Code global settings stored as .claude-profile.json."""

    schema_version: str = Field(default="claude-profile-v1", alias="$schema")
    settings: dict = Field(default_factory=dict)
    claude_md: str | None = None
    mcp_servers: dict[str, dict] = Field(default_factory=dict)
    model_config = {"populate_by_name": True}


class ActivationResult(BaseModel):
    """Result of activating a workspace."""

    workspace_path: str
    client_name: str
    auto_synced: bool = False
    auto_sync_client: str | None = None
    identity_verified: bool = False
    identity_warnings: list[str] = Field(default_factory=list)
    warnings: list[str] = Field(default_factory=list)
    backup_id: str | None = None


class DeactivationResult(BaseModel):
    """Result of deactivating a workspace."""

    previous_workspace: str | None = None
    auto_synced: bool = False


class ActiveWorkspace(BaseModel):
    """Currently active workspace info."""

    workspace_path: str
    client_name: str
    activated_at: str


class DriftReport(BaseModel):
    """Drift detection between live global config and stored profile."""

    has_drift: bool
    settings_changed: bool
    claude_md_changed: bool
    mcp_servers_changed: bool
    active_hash: str
    profile_hash: str


class HealthCheck(BaseModel):
    """Single health check result."""

    name: str
    status: str  # "pass", "warn", "fail"
    message: str


class HealthReport(BaseModel):
    """Full workspace health report."""

    workspace_path: str
    client_name: str
    overall: str  # "healthy", "degraded", "broken"
    checks: list[HealthCheck] = Field(default_factory=list)
    timestamp: str = ""


class ImportResult(BaseModel):
    """Result of importing a workspace bundle."""

    client_name: str
    target_path: str
    files_written: list[str] = Field(default_factory=list)
    files_skipped: list[str] = Field(default_factory=list)
    files_merged: list[str] = Field(default_factory=list)
    warnings: list[str] = Field(default_factory=list)


# --- Boards (Vibe Kanban) ---


class BoardScope(str, Enum):
    GLOBAL = "global"
    CLIENT = "client"


class CardPriority(str, Enum):
    LOW = "low"
    MEDIUM = "medium"
    HIGH = "high"
    URGENT = "urgent"


class CardStatus(str, Enum):
    ACTIVE = "active"
    ARCHIVED = "archived"


class ExecutionState(str, Enum):
    IDLE = "idle"
    RUNNING = "running"
    SUCCESS = "success"
    ERROR = "error"


class CardExecution(BaseModel):
    """Tracks a Claude Code execution triggered by a card."""

    state: ExecutionState = ExecutionState.IDLE
    started_at: str | None = None
    finished_at: str | None = None
    output: str = ""
    error: str | None = None
    duration_ms: float = 0.0
    cost_usd: float = 0.0
    input_tokens: int = 0
    output_tokens: int = 0
    session_id: str | None = None


class CardComment(BaseModel):
    """A comment on a card."""
    id: str = ""
    card_id: str = ""
    author: str = ""
    text: str = ""
    parent_id: str | None = None  # for threading
    created: str = ""
    updated: str = ""
    reactions: dict[str, list[str]] = Field(default_factory=dict)  # emoji -> [author_names]


class RelationshipType(str, Enum):
    BLOCKS = "blocks"
    BLOCKED_BY = "blocked_by"
    RELATED = "related"
    DUPLICATE = "duplicate"


class CardRelationship(BaseModel):
    """A relationship between two cards."""
    id: str = ""
    source_card_id: str = ""
    target_card_id: str = ""
    type: RelationshipType = RelationshipType.RELATED


class Card(BaseModel):
    id: str = ""
    column_id: str = ""
    title: str = ""
    description: str = ""
    labels: list[str] = Field(default_factory=list)
    priority: CardPriority = CardPriority.MEDIUM
    assignee: str | None = None
    project_path: str | None = None
    due_date: str | None = None
    position: int = 0
    status: CardStatus = CardStatus.ACTIVE
    created: str = ""
    updated: str = ""
    completed: str | None = None
    execution: CardExecution = Field(default_factory=CardExecution)
    comments: list[CardComment] = Field(default_factory=list)
    parent_card_id: str | None = None
    sub_card_sort_order: int | None = None


class Column(BaseModel):
    id: str = ""
    name: str = ""
    color: str | None = None
    wip_limit: int | None = None
    hidden: bool = False
    sort_order: int = 0


class Board(BaseModel):
    id: str = ""
    name: str = ""
    scope: BoardScope = BoardScope.CLIENT
    client_name: str | None = None
    columns: list[Column] = Field(default_factory=list)
    cards: list[Card] = Field(default_factory=list)
    relationships: list[CardRelationship] = Field(default_factory=list)
    created: str = ""
    updated: str = ""
