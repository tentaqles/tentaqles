import pandas as pd

ORDERS = "data/acme/orders.parquet"
orders = pd.read_parquet(ORDERS)
features = pd.read_csv("data/features.csv")
model.fit(X, y)
