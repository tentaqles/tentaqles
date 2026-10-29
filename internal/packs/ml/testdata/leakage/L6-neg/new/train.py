from sklearn.model_selection import train_test_split

df = df.sort_values("order_date")
X_train, X_test, y_train, y_test = train_test_split(X, y, test_size=0.2, shuffle=False)
model.fit(X_train, y_train)
