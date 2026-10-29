from sklearn.ensemble import RandomForestClassifier

y = df["churn"]
X = df
model = RandomForestClassifier(random_state=0)
model.fit(X, y)
X_small = df[["tenure_days", "churn"]]
model.fit(X_small, y)
