from sklearn.ensemble import RandomForestClassifier

TARGET = "churn"
FEATURES = ["tenure_days", "plan"]
y = df[TARGET]
X = df.drop(columns=[TARGET])
model = RandomForestClassifier(random_state=0)
model.fit(X, y)
X_small = df[FEATURES]
model.fit(X_small, y)
