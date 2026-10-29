import torch
from torch.utils.data import DataLoader

torch.manual_seed(0)
torch.use_deterministic_algorithms(True)
g = torch.Generator()
g.manual_seed(0)
loader = DataLoader(ds, batch_size=32, shuffle=True, num_workers=4, worker_init_fn=seed_worker, generator=g)
for epoch in range(3):
    optimizer.step()
