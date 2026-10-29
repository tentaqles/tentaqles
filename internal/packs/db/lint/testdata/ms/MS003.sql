-- want: MS003:error:3 MS003:error:4 MS003:warn:5
SET LOCK_TIMEOUT 5000
ALTER TABLE dbo.Orders ALTER COLUMN Code varchar(10) NULL
ALTER TABLE dbo.Orders ALTER COLUMN Qty bigint NULL
ALTER TABLE dbo.Customers ALTER COLUMN Name nvarchar(80) NOT NULL
