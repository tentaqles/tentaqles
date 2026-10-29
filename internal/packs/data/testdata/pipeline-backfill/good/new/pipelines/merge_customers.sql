MERGE INTO analytics.customers t
USING staging.customers s
ON t.customer_id = s.customer_id
WHEN MATCHED THEN UPDATE SET updated_at = CURRENT_TIMESTAMP
WHEN NOT MATCHED THEN INSERT (customer_id, updated_at) VALUES (s.customer_id, CURRENT_TIMESTAMP);
