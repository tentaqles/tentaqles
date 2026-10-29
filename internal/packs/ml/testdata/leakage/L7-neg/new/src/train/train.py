from sklearn.model_selection import GroupShuffleSplit

groups = df["customer_id"]
gss = GroupShuffleSplit(n_splits=1, random_state=0)
train_idx, test_idx = next(gss.split(X, y, groups=groups))
model.fit(X.iloc[train_idx], y.iloc[train_idx])
