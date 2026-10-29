from sklearn.preprocessing import StandardScaler
from sklearn.linear_model import LogisticRegression
from sklearn.model_selection import cross_val_score, GridSearchCV
from sklearn.svm import SVC

X = df.drop(columns=["churn"])
y = df["churn"]
X_s = StandardScaler().fit_transform(X)
scores = cross_val_score(LogisticRegression(), X_s, y, cv=5)
search = GridSearchCV(SVC(), {"C": [1, 10]})
search.fit(X_s, y)
