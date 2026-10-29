import mlflow

mlflow.sklearn.autolog()
with mlflow.start_run():
    model.fit(X, y)
