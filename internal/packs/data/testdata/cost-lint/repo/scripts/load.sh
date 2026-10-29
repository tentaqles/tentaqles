#!/usr/bin/env bash
set -euo pipefail
# Synthetic fixture for data/bq-job-max-bytes (acme).
bq query --use_legacy_sql=false 'SELECT * FROM acme.orders'
bq query --use_legacy_sql=false --maximum_bytes_billed=1000000 'SELECT 1'
bq query --maximum_bytes_billed=900000000000 'SELECT 2'
bq query --dry_run 'SELECT 3'
bq query \
  --use_legacy_sql=false \
  'SELECT 4'
# tq:allow bq-cli-no-cap: one-off backfill reviewed by tester
bq query 'SELECT 5'
bq ls acme
