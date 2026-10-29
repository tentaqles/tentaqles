def build(df):
    df["lag_7"] = df["sales"].shift(7)
    df["avg_7"] = df["sales"].rolling(7).mean()
    df["target"] = df["sales"].shift(-1)
    df = df.ffill()
    return df
