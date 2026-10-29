import json
import pickle as pkl
from json import load
from torch import load as tload
from huggingface_hub import hf_hub_download

cfg = load(open(sys.argv[1]))
meta = json.load(fh)
clf = pkl.load(open("models/churn.pkl", "rb"))
weights = tload(hf_hub_download("acme/churn", "model.pt"), weights_only=True)
