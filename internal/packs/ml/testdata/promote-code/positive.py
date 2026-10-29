import mlflow
from mlflow import MlflowClient

client = MlflowClient()
client.transition_model_version_stage("churn", 7, "Production")
client.transition_model_version_stage(name="churn", version=7, stage="production")
client.set_registered_model_alias("churn", "Production", "7")
sm.update_model_package(ModelPackageArn=arn, ModelApprovalStatus="Approved")
client.set_registered_model_alias("churn", "staging", "7")
client.transition_model_version_stage("churn", 7, "Staging")
sm.update_model_package(ModelPackageArn=arn, ModelApprovalStatus="Rejected")

client.set_registered_model_alias("churn", alias, "7")
