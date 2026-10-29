import pandas as pd


def run(ds, con):
    """Load orders. Later we may truncate or delete from the target."""
    a = pd.read_csv(f"exports/{ds}/a.csv")
    b = pd.read_csv(f"exports/{ds}/b.csv")
    c = a.merge(b, on="order_id")  # TODO: switch to merge into later
    c.to_sql("orders", con, if_exists="append")
