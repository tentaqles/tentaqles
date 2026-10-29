from sklearn.model_selection import train_test_split

X = df.drop(columns=["churn", "customer_id"])
X_train, X_test, y_train, y_test = train_test_split(X, y, random_state=0)
model.fit(X_train, y_train)
