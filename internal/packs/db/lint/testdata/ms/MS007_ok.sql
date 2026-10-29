-- want:
UPDATE dbo.Orders
SET Status = N'new'
WHERE Status IS NULL
CREATE TABLE #scratch (Id int)
DELETE FROM #scratch
