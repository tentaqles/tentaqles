-- want: MS002:error:3 MS006:error:3
SET XACT_ABORT ON
IF COL_LENGTH('dbo.Orders', 'Region') IS NULL ALTER TABLE dbo.Orders ADD Region nvarchar(10) NOT NULL
