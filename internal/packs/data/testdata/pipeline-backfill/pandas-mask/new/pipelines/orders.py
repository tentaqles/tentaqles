import pandas as pd
from datetime import date

df = pd.read_parquet("s3://acme/orders")
out = df[df["order_date"] == date.today()]
today = date.today()
out2 = df[df.order_date >= today]
out3 = df[(df.region == "eu") & (df.order_date < today)]
out.to_parquet("s3://acme/out")
