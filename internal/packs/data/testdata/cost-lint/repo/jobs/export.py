"""Synthetic fixture for data/bq-job-max-bytes (acme)."""
import subprocess

import pandas as pd
import pandas_gbq
from google.cloud import bigquery

client = bigquery.Client(project="acme-dev")
capped = bigquery.Client(
    project="acme-dev",
    default_query_job_config=bigquery.QueryJobConfig(maximum_bytes_billed=10**9),
)

open_cfg = bigquery.QueryJobConfig(use_legacy_sql=False)
late_cfg = bigquery.QueryJobConfig()
late_cfg.maximum_bytes_billed = 1_000_000
big_cfg = bigquery.QueryJobConfig(maximum_bytes_billed=900000000000)


def run():
    client.query("SELECT * FROM acme.orders")
    client.query("SELECT 1", job_config=late_cfg)
    capped.query("SELECT 2")
    frame = pd.DataFrame({"a": [1]})
    frame.query("a > 0")
    pandas_gbq.read_gbq("SELECT 3", project_id="acme-dev")
    pd.read_gbq(
        "SELECT 4",
        configuration={"query": {"maximumBytesBilled": "1000000"}},
    )
    client.query("SELECT 5")  # tq:allow bq-query-no-cap: tiny lookup table
    subprocess.run(["bq", "query", "--use_legacy_sql=false", "SELECT 6"], check=True)
