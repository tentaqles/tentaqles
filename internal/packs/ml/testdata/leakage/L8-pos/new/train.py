import torch


def evaluate(model, val_loader):
    total = 0
    for xb, yb in val_loader:
        total += model(xb).sum()
    return total


for epoch in range(3):
    optimizer.step()
