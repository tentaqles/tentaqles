import torch
from torch.utils.data import DataLoader

torch.manual_seed(0)
loader = DataLoader(ds, batch_size=32, shuffle=True, num_workers=4)
for epoch in range(3):
    optimizer.step()
