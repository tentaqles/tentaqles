import pandas as pd

events = spark.read.format("delta").option("versionAsOf", 12).load("data/acme/events")
history = spark.read.option("timestampAsOf", "2026-01-01").table("acme.history")
recent = pd.read_sql("SELECT id, amount FROM orders FOR SYSTEM_TIME AS OF '2026-01-01'", conn)
model.fit(X, y)
