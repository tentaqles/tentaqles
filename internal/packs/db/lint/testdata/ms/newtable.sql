-- want:
CREATE TABLE [dbo].[AcmeItems] ([Id] int NOT NULL, [Sku] nvarchar(20) NULL)
GO
ALTER TABLE dbo.AcmeItems ADD Qty int NOT NULL
CREATE INDEX IX_AcmeItems_Sku ON dbo.AcmeItems (Sku)
