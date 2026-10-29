import torch

torch.manual_seed(0)
torch.cuda.manual_seed_all(0)
torch.use_deterministic_algorithms(True)
model = model.to("cuda")
for epoch in range(3):
    optimizer.step()
