"""Not a BigQuery file: .query() here is pandas."""
import pandas as pd

df = pd.DataFrame({"a": [1, 2]})
print(df.query("a > 1"))
