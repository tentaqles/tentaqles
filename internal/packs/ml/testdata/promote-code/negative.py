"""Registry helpers. Promotion goes through tq pack ml promote, never
client.set_registered_model_alias("churn", "production", "7") from here."""
from mlflow import MlflowClient

client = MlflowClient()
# client.transition_model_version_stage("churn", 7, "Production")
mv = client.get_model_version_by_alias("churn", "production")
note = "call set_registered_model_alias('churn', 'production', '7') only from scripts/promote.py"
client.set_registered_model_alias("churn", "challenger", mv.version)
