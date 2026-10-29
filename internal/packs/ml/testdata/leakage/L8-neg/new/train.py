import torch


def evaluate(model, val_loader):
    model.eval()
    total = 0
    with torch.no_grad():
        for xb, yb in val_loader:
            total += model(xb).sum()
    return total


@torch.inference_mode()
def test(model, test_loader):
    model.eval()
    for xb in test_loader:
        model(xb)


for epoch in range(3):
    optimizer.step()
