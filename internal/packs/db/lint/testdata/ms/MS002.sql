-- want: MS002:error:3 MS002:error:4
SET LOCK_TIMEOUT 5000
ALTER TABLE dbo.Orders ADD Region nvarchar(10) NOT NULL,
  Channel int NOT NULL
GO
