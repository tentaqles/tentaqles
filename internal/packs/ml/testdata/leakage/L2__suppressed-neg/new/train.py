from sklearn.calibration import CalibratedClassifierCV

calibrator = CalibratedClassifierCV(base, cv="prefit")
# tq:allow L2: the prefit calibrator is fitted on the validation split by design
calibrator.fit(X_val, y_val)
