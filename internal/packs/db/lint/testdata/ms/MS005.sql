-- want: MS005:error:3 MS005:error:4 MS005:error:5
SET LOCK_TIMEOUT 5000
ALTER TABLE dbo.Orders DROP COLUMN Legacy
DROP TABLE dbo.OldOrders
EXEC sp_rename 'dbo.Orders.Note', 'Memo', 'COLUMN'
