from xgboost import XGBClassifier

model = XGBClassifier(random_state=0)
model.fit(X_train, y_train, eval_set=[(X_val, y_val)], verbose=False)
values = df.values
model.fit(values, y_train)
