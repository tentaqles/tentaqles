HORIZON = 7


def build(df, n):
    df["next_week_sales"] = df["sales"].shift(-HORIZON)
    df["next_n"] = df["sales"].shift(periods=-n)
    df["prev_n"] = df["sales"].shift(n)
    return df
