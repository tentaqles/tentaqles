import pandas as pd

orders = pd.read_parquet("data/acme/latest/orders.parquet")
events = spark.table("acme.events")
recent = pd.read_sql("SELECT id, amount FROM orders", conn)
model.fit(X, y)
