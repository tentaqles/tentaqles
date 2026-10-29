-- want:
SET LOCK_TIMEOUT 5000
ALTER TABLE dbo.Orders ADD Region nvarchar(10) NOT NULL CONSTRAINT DF_Orders_Region DEFAULT N'eu', Note nvarchar(50) NULL
GO
