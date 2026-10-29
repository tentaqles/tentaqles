INSERT INTO analytics.payments
SELECT payment_id, amount FROM staging.payments;
