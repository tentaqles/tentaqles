-- want: SQL001:error:3
SET LOCK_TIMEOUT 5000;
SELECT 1 /*! DROP TABLE orders */;
