-- want: PG006:error:3 PG006:error:4
SET lock_timeout = '5s';
ALTER TABLE orders ADD CONSTRAINT orders_customer_fk FOREIGN KEY (customer_id) REFERENCES customers (id);
ALTER TABLE orders ADD CHECK (qty > 0);
