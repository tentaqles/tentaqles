"""Checks over the dbt-contract-starter files. Stdlib only: no dbt, no tq, no
warehouse. The Go side (internal/packs/data, TestTemplateDBTContractStarter)
runs the real tq built-in checks over this template; these tests keep the
template's own promises when someone edits it without Go at hand."""

import csv
import re
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
MODELS = ROOT / "models"


def read(rel: str) -> str:
    return (ROOT / rel).read_text(encoding="utf-8")


def sql_without_comments(text: str) -> str:
    text = re.sub(r"--[^\n]*", "", text)
    text = re.sub(r"/\*.*?\*/", "", text, flags=re.S)
    return re.sub(r"\{#.*?#\}", "", text, flags=re.S)


def test_layout():
    for rel in [
        "dbt_project.yml",
        "tq-packs.yaml",
        ".sqlfluff",
        "profiles.example.yml",
        "contracts/fct_orders.odcs.yaml",
        "models/staging/stg_orders.sql",
        "models/staging/stg_customers.sql",
        "models/staging/_staging.yml",
        "models/marts/fct_orders.sql",
        "models/marts/_marts.yml",
        "seeds/raw_customers.csv",
        "seeds/raw_orders.csv",
    ]:
        assert (ROOT / rel).is_file(), rel
    assert not (ROOT / "profiles.yml").exists(), "never commit a real profiles.yml"


def test_seeds_are_synthetic():
    with (ROOT / "seeds" / "raw_customers.csv").open(encoding="utf-8") as f:
        rows = list(csv.DictReader(f))
    assert rows
    for row in rows:
        assert row["customer_name"].startswith("tester"), row["customer_name"]
        assert row["email"].endswith("@acme.test"), row["email"]


def test_no_select_star_or_literal_relations():
    for path in MODELS.rglob("*.sql"):
        sql = sql_without_comments(path.read_text(encoding="utf-8"))
        assert not re.search(r"select\s+\*", sql, re.I), path
        # every FROM/JOIN target is a ref(), source(), this or a CTE/alias name
        for m in re.finditer(r"\b(?:from|join)\s+([^\s(]+)", sql, re.I):
            target = m.group(1)
            assert target.startswith("{{") or "." not in target, (path, target)


def test_mart_contract_is_enforced_with_types():
    marts = read("models/marts/_marts.yml")
    assert re.search(r"contract:\s*\n\s+enforced:\s*true", marts)
    assert "+contract:" in read("dbt_project.yml")
    columns = re.findall(r"- name: (\w+)\n(?:\s+description:[^\n]*\n)?\s+data_type: ", marts)
    assert set(columns) == {"order_id", "customer_id", "order_date", "status", "amount", "updated_at"}


def test_keys_are_tested():
    staging = read("models/staging/_staging.yml")
    for key in ("customer_id", "order_id"):
        assert re.search(rf"- name: {key}\n\s+description:[^\n]*\n\s+data_tests: \[unique, not_null\]", staging), key
    assert re.search(r"- name: order_id\n(?:.*\n){1,6}?\s+data_tests: \[unique, not_null\]", read("models/marts/_marts.yml"))


def test_unit_test_present():
    marts = read("models/marts/_marts.yml")
    assert "unit_tests:" in marts and "model: fct_orders" in marts
    assert "is_incremental: false" in marts


def test_incremental_has_key_and_lookback():
    sql = read("models/marts/fct_orders.sql")
    assert "materialized='incremental'" in sql
    assert "unique_key='order_id'" in sql
    block = sql.split("{% if is_incremental() %}")[1].split("{% endif %}")[0]
    assert "max(" in block and "interval" in block


def test_contract_matches_the_model():
    contract = read("contracts/fct_orders.odcs.yaml")
    assert re.search(r"^apiVersion: v3\.0\.\d+$", contract, re.M)
    assert re.search(r"^kind: DataContract$", contract, re.M)
    for field in ("id", "version", "status"):
        assert re.search(rf"^{field}: \S+", contract, re.M), field
    assert "property: dbtModel\n        value: fct_orders" in contract
    props = re.findall(r"^      - name: (\w+)$", contract, re.M)
    assert set(props) == {"order_id", "customer_id", "order_date", "status", "amount", "updated_at"}
    assert "primaryKey: true" in contract
    # no personal data is classified here: the mart carries keys and amounts only
    assert not re.search(r"classification: (pii|sensitive|restricted)", contract)


def test_contract_types_match_the_model_exactly():
    # data/contract-dbt-sync compares physicalType with data_type, so the two
    # spell every type the same way (numeric(12,2), not numeric(12, 2))
    contract = read("contracts/fct_orders.odcs.yaml")
    physical = dict(re.findall(r"^      - name: (\w+)\n(?:(?!      - name:).*\n)*?\s+physicalType: ([^\n]+)$", contract, re.M))
    marts = read("models/marts/_marts.yml")
    dbt_types = dict(re.findall(r"- name: (\w+)\n(?:\s+description:[^\n]*\n)?\s+data_type: ([^\n]+)", marts))
    assert physical and physical == dbt_types, (physical, dbt_types)


def test_pack_config():
    cfg = read("tq-packs.yaml")
    assert 'contract_paths: ["models/marts/**"]' in cfg
    assert "prod_targets: [prod, production]" in cfg
    assert "tool_paths" not in cfg, "tool_paths is trusted-layer only"
    sqlfluff = read(".sqlfluff")
    assert "templater = jinja" in sqlfluff and "dialect = postgres" in sqlfluff
