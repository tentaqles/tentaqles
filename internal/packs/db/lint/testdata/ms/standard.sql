-- want: MS001:warn:3 | opts: edition=standard
SET LOCK_TIMEOUT 5000
CREATE INDEX IX_Orders_Status ON dbo.Orders (Status)
