-- TODO: truncate first, or use MERGE INTO
INSERT INTO analytics.refunds
SELECT refund_id, amount FROM staging.refunds WHERE note <> 'delete from';
