-- want: MS002:error:3 MS002:error:4 MS006:error:3
SET XACT_ABORT ON
IF COL_LENGTH('dbo.Orders', 'Region') IS NULL BEGIN ALTER TABLE dbo.Orders ADD Region nvarchar(10) NOT NULL END
IF COL_LENGTH('dbo.Orders', 'Zone') IS NULL BEGIN ALTER TABLE dbo.Orders ADD Zone int NULL; ALTER TABLE dbo.Orders ADD Code int NOT NULL; END
IF COL_LENGTH('dbo.Orders', 'Note') IS NULL BEGIN TRY ALTER TABLE dbo.Orders ADD Note int NULL END TRY BEGIN CATCH THROW; END CATCH
