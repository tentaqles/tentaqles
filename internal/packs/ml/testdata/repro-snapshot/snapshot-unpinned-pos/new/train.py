import pandas as pd

raw = pd.read_csv("data/raw/orders.csv")
draft = pd.read_parquet("data/acme/draft.parquet")
model.fit(X, y)
