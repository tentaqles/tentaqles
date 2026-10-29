from sklearn.preprocessing import StandardScaler
from sklearn.linear_model import LogisticRegression
from sklearn.model_selection import cross_val_score

X = df.drop(columns=["churn"])
y = df["churn"]
scores = cross_val_score(LogisticRegression(), StandardScaler().fit_transform(X), y, cv=5)
model = LogisticRegression()
model.fit(X, y)
