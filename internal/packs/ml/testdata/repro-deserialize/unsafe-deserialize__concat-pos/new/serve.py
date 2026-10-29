import joblib

rel = "models/churn.pkl"
root = sys.argv[1]
a = joblib.load("/" + rel)
b = joblib.load(root + "/churn.pkl")
c = joblib.load("models/" + rel)
