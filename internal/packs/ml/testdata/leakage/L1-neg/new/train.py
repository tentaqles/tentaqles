import pandas as pd
from sklearn.preprocessing import StandardScaler
from sklearn.model_selection import train_test_split
from sklearn.linear_model import LogisticRegression

df = pd.read_csv("data/acme_orders.csv")
X = df.drop(columns=["churn"])
y = df["churn"]
X_train, X_test, y_train, y_test = train_test_split(X, y, random_state=0)
scaler = StandardScaler()
X_train_s = scaler.fit_transform(X_train)
X_test_s = scaler.transform(X_test)
parts = "a,b".split(",")
model = LogisticRegression()
model.fit(X_train_s, y_train)
