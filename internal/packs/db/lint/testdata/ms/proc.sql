-- want:
CREATE OR ALTER PROCEDURE dbo.acme_cleanup AS
BEGIN
  UPDATE dbo.Orders SET Status = 'x';
  DELETE FROM dbo.Orders;
  DROP TABLE dbo.Temp;
END
GO
