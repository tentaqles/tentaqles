-- want: MS007:error:3 MS007:error:6
SET LOCK_TIMEOUT 5000
UPDATE dbo.Orders
SET Status = N'new'
GO
DELETE FROM dbo.Orders
