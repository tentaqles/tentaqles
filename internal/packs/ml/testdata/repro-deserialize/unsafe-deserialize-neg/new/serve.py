import joblib
import torch

model = joblib.load("models/churn.joblib")
weights = torch.load(weights_path, weights_only=True)
with open("models/encoder.pkl", "rb") as fh:
    enc = joblib.load(fh)
