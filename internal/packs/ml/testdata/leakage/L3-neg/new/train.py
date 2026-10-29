from sklearn.pipeline import make_pipeline
from sklearn.preprocessing import StandardScaler
from sklearn.linear_model import LogisticRegression
from sklearn.model_selection import cross_val_score

X = df.drop(columns=["churn"])
y = df["churn"]
pipe = make_pipeline(StandardScaler(), LogisticRegression())
scores = cross_val_score(pipe, X, y, cv=5)
X_s = StandardScaler().fit_transform(X)
X_s = df.drop(columns=["churn", "id"])
pipe.fit(X_s, y)
scores = cross_val_score(pipe, X_s, y, cv=5)
