from sklearn.ensemble import GradientBoostingClassifier

model = GradientBoostingClassifier(random_state=0)
model.fit(X_test, y_test)
print(model.score(X_test, y_test))
