INSERT OVERWRITE analytics.customer_export3
SELECT customer_id, acme.mask_email(email) AS email_masked FROM crm.customers;
