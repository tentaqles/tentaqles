-- want: MS002:error:3 MS006:error:3
IF NOT EXISTS (SELECT 1 FROM sys.columns WHERE Name = N'Region' AND Object_ID = Object_ID(N'dbo.Orders'))
  ALTER TABLE dbo.Orders ADD Region nvarchar(10) NOT NULL
