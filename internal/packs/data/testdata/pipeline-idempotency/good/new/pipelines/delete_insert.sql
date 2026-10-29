DELETE FROM analytics.payments WHERE payment_date = '{{ ds }}';
INSERT INTO analytics.payments
SELECT payment_id, amount FROM staging.payments WHERE payment_date = '{{ ds }}';
