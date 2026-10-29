-- want:
SET lock_timeout = '5s';
ALTER TABLE orders ADD CONSTRAINT region_nn CHECK (region IS NOT NULL) NOT VALID;
ALTER TABLE orders VALIDATE CONSTRAINT region_nn;
ALTER TABLE orders ALTER COLUMN region SET NOT NULL;
