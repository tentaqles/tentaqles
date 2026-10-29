import pandas as pd
from datetime import date

def run(ds):
    df = pd.read_parquet("s3://acme/orders")
    df["loaded_at"] = date.today()
    df["is_recent"] = df["order_date"] >= date.today()
    first = df[0:10]
    cols = df[["order_id", "order_date"]]
    out = df[df["order_date"] == ds]
    out.to_parquet("s3://acme/out")
