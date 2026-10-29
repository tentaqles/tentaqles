def spend(df):
    return df.groupby("customer_id")["amount"].sum()
