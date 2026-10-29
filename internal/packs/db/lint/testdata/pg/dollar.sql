-- want:
CREATE FUNCTION acme_touch() RETURNS trigger LANGUAGE plpgsql AS $fn$
BEGIN
  UPDATE orders SET touched = now();
  DELETE FROM orders;
  RETURN NEW;
END
$fn$;
