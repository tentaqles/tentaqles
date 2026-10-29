import sys

import joblib as jl
import pickle as pkl
from joblib import load as jl_load
from torch import load
from torch.serialization import load as tload

clf = pkl.load(open(sys.argv[1], "rb"))
blob_model = pkl.loads(blob)
pipe = jl.load(sys.argv[2])
enc = jl_load(sys.argv[3])
weights = load(sys.argv[4])
other = tload(sys.argv[5])
