import pickle
import sys

import joblib
import torch
from huggingface_hub import hf_hub_download

model = joblib.load(sys.argv[1])
weights = torch.load(hf_hub_download("acme/churn", "model.pt"))
with open("/srv/models/churn.pkl", "rb") as fh:
    clf = pickle.load(fh)
