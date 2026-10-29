import numpy as np
from sklearn.ensemble import RandomForestClassifier
from sklearn.model_selection import train_test_split, KFold

rng = np.random.default_rng(7)
X_train, X_test, y_train, y_test = train_test_split(X, y, random_state=7)
cv = KFold(n_splits=5)
model = RandomForestClassifier(n_estimators=100, random_state=7)
params = {"n_estimators": 10}
other = RandomForestClassifier(**params)
model.fit(X_train, y_train)
