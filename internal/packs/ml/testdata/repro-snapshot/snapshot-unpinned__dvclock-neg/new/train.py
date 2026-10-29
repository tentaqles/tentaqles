import pandas as pd

train = pd.read_parquet("data/prepared/train.parquet")
model.fit(X, y)
