"""End-to-end example: Postgres -> DataFrame -> Jev labels -> staging table.

    tq dotenv run -- python examples/label_tickets.py            # needs TYPESAFE_API_KEY + DATABASE_URL
    tq dotenv run -- python examples/label_tickets.py --dry-run  # cost estimate only, still reads the DB
"""

from __future__ import annotations

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent.parent))

from jev_labeller import JevClient, Labeller, LabelTask, SqliteCache  # noqa: E402
from jev_labeller.pg import connect, read_query, write_labels  # noqa: E402

TASK = LabelTask.from_json(Path(__file__).with_name("task.json"))


def main() -> int:
    dry = "--dry-run" in sys.argv
    conn = connect()
    df = read_query(
        conn,
        "select ticket_id, subject, body from support.tickets where created_at > now() - %s::interval",
        ("7 days",),
    )
    res = Labeller(JevClient(), TASK, cache=SqliteCache("labels.sqlite")).label(
        df, dry_run=dry, review_path=None if dry else "review.csv"
    )
    print(res.stats.summary())
    if dry:
        return 0
    cols = [c for c in res.df.columns if c.startswith(TASK.name)]
    n = write_labels(conn, res.df, "staging.ticket_topics", "ticket_id", cols)
    print(f"wrote {n} rows to staging.ticket_topics; {len(res.review)} rows in review.csv")
    return 0


if __name__ == "__main__":
    sys.exit(main())
