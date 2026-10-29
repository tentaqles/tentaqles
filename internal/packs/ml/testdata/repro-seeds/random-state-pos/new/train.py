import numpy as np
from sklearn.ensemble import RandomForestClassifier
from sklearn.model_selection import train_test_split, KFold

np.random.seed(0)
X_train, X_test, y_train, y_test = train_test_split(X, y)
cv = KFold(n_splits=5, shuffle=True)
model = RandomForestClassifier(n_estimators=100)
model.fit(X_train, y_train)
