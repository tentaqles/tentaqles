-- want:
CREATE TABLE acme_items (id bigint PRIMARY KEY, sku text);
ALTER TABLE acme_items ADD COLUMN qty int NOT NULL;
ALTER TABLE acme_items ADD CONSTRAINT acme_items_sku_key UNIQUE (sku);
ALTER TABLE acme_items ALTER COLUMN sku TYPE varchar(10);
CREATE INDEX acme_items_sku_idx ON acme_items (sku);
