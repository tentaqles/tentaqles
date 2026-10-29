-- want: MS004:error:3 MS004:error:4
SET LOCK_TIMEOUT 5000;
ALTER TABLE dbo.Orders ADD CONSTRAINT FK_Orders_Customers FOREIGN KEY (CustomerId) REFERENCES dbo.Customers (Id);
ALTER TABLE dbo.Orders WITH CHECK ADD CONSTRAINT CK_Orders_Qty CHECK (Qty > 0);
