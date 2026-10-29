-- want: MS001:error:3 MS001:error:4
SET LOCK_TIMEOUT 5000;
CREATE NONCLUSTERED INDEX IX_Orders_Status ON dbo.Orders (Status);
ALTER INDEX IX_Orders_Code ON [dbo].[Orders] REBUILD;
