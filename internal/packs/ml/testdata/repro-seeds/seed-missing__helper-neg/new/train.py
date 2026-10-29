from common.seed import set_seed
from sklearn.ensemble import RandomForestClassifier

set_seed(42)
model = RandomForestClassifier(random_state=0)
model.fit(X_train, y_train)
