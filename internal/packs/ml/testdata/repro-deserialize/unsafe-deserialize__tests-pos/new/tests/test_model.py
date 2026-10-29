import pickle


def test_roundtrip(tmp_path):
    p = tmp_path / "m.pkl"
    with open(p, "rb") as fh:
        assert pickle.load(fh) is not None
