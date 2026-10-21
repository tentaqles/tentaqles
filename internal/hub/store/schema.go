package store

const (
	StatusQueued     = "queued"
	StatusRunning    = "running"
	StatusWaiting    = "waiting"
	StatusFinished   = "finished"
	StatusFailed     = "failed"
	StatusCancelled  = "cancelled"
	StatusOverBudget = "over-budget"
)

// Statuses is the one status vocabulary used by the store, the CLI and the page.
var Statuses = []string{StatusQueued, StatusRunning, StatusWaiting, StatusFinished, StatusFailed, StatusCancelled, StatusOverBudget}

var CardKinds = []string{"project", "feature", "fix", "analysis", "hotfix", "chore"}

const (
	LaneBacklog = "backlog"
	LaneTodo    = "todo"
	LaneActive  = "active"
	LaneReview  = "review"
	LaneDone    = "done"
	LaneOther   = "other"
)

var LaneKinds = []string{LaneBacklog, LaneTodo, LaneActive, LaneReview, LaneDone, LaneOther}

// TagAgentReady is the only tag with behaviour: entering an active lane enqueues a run.
const TagAgentReady = "agent-ready"

// migrations[i] takes the schema from version i to i+1. Append only; never edit a shipped entry.
var migrations = []string{schemaV1}

const schemaV1 = `
CREATE TABLE lanes (
  name TEXT PRIMARY KEY COLLATE NOCASE,
  position INTEGER NOT NULL,
  kind TEXT NOT NULL CHECK (kind IN ('backlog','todo','active','review','done','other'))
);
INSERT INTO lanes(name, position, kind) VALUES
  ('backlog', 0, 'backlog'), ('todo', 1, 'todo'), ('in progress', 2, 'active'), ('review', 3, 'review'), ('done', 4, 'done');
CREATE TABLE cards (
  id TEXT PRIMARY KEY,
  title TEXT NOT NULL,
  body TEXT NOT NULL DEFAULT '',
  lane TEXT NOT NULL,
  kind TEXT NOT NULL CHECK (kind IN ('project','feature','fix','analysis','hotfix','chore')),
  tags TEXT NOT NULL DEFAULT '[]',
  parent_id TEXT REFERENCES cards(id),
  project_dir TEXT NOT NULL DEFAULT '',
  external_provider TEXT, external_id TEXT, external_url TEXT, external_lane TEXT,
  locked_by_run TEXT,
  created_at TEXT NOT NULL, updated_at TEXT NOT NULL, done_at TEXT
);
CREATE INDEX cards_lane ON cards(lane);
CREATE INDEX cards_parent ON cards(parent_id);
CREATE TABLE comments (
  id TEXT PRIMARY KEY,
  card_id TEXT NOT NULL REFERENCES cards(id),
  author_kind TEXT NOT NULL CHECK (author_kind IN ('human','agent','system')),
  author TEXT NOT NULL,
  body TEXT NOT NULL,
  run_id TEXT,
  created_at TEXT NOT NULL
);
CREATE INDEX comments_card ON comments(card_id, created_at);
CREATE TABLE decisions (
  id TEXT PRIMARY KEY,
  card_id TEXT NOT NULL REFERENCES cards(id),
  run_id TEXT, slug TEXT,
  kind TEXT NOT NULL CHECK (kind IN ('plan-approval','result','judge','question','permission')),
  checkpoint TEXT, reviewer TEXT,
  text TEXT NOT NULL DEFAULT '',
  options TEXT NOT NULL DEFAULT '[]',
  status TEXT NOT NULL CHECK (status IN ('pending','answered')),
  choice TEXT, note TEXT, actor TEXT, asked_at TEXT, answered_at TEXT,
  data TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX decisions_status ON decisions(status, asked_at);
CREATE INDEX decisions_card ON decisions(card_id);
CREATE TABLE attachments (
  id TEXT PRIMARY KEY,
  card_id TEXT NOT NULL REFERENCES cards(id),
  path TEXT NOT NULL,
  label TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);
CREATE TABLE runs (
  id TEXT PRIMARY KEY,
  card_id TEXT NOT NULL REFERENCES cards(id),
  workspace TEXT NOT NULL,
  adapter TEXT NOT NULL CHECK (adapter IN ('claude','process')),
  on_ramp TEXT NOT NULL CHECK (on_ramp IN ('kickoff','feature','fix','build','resume')),
  project_dir TEXT NOT NULL DEFAULT '',
  worktree TEXT, slug TEXT, session_id TEXT,
  status TEXT NOT NULL CHECK (status IN ('queued','running','waiting','finished','failed','cancelled','over-budget')),
  budget TEXT NOT NULL,
  usage TEXT NOT NULL DEFAULT '{}',
  cost_usd REAL NOT NULL DEFAULT 0,
  rounds INTEGER NOT NULL DEFAULT 0,
  seconds REAL NOT NULL DEFAULT 0,
  started_at TEXT, ended_at TEXT,
  log_path TEXT NOT NULL DEFAULT '',
  state TEXT, stop_reason TEXT,
  queued_at TEXT NOT NULL, not_before TEXT NOT NULL, updated_at TEXT NOT NULL,
  phase TEXT NOT NULL DEFAULT 'start',
  retried INTEGER NOT NULL DEFAULT 0,
  relocations TEXT NOT NULL DEFAULT '[]',
  allowed_tools TEXT NOT NULL DEFAULT '[]',
  round_log TEXT NOT NULL DEFAULT '[]',
  kind TEXT NOT NULL DEFAULT ''
);
CREATE INDEX runs_status ON runs(status, queued_at);
CREATE INDEX runs_card ON runs(card_id);
CREATE TABLE activity (
  ts TEXT NOT NULL, workspace TEXT NOT NULL, actor TEXT NOT NULL, verb TEXT NOT NULL,
  card_id TEXT, run_id TEXT, data TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX activity_ts ON activity(ts);
CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
`
