import mlflow

with mlflow.start_run():
    mlflow.log_params({"n_estimators": 100})
    model.fit(X, y)
    mlflow.log_metric("auc", auc)
