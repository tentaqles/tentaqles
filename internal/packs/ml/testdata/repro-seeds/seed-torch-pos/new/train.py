import numpy as np
import torch

np.random.seed(0)
torch.use_deterministic_algorithms(True)
for epoch in range(3):
    optimizer.step()
