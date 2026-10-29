def build(df):
    df["next_week_sales"] = df["sales"].shift(-7)
    df["smooth"] = df["sales"].rolling(7, center=True).mean()
    df = df.bfill()
    return df
