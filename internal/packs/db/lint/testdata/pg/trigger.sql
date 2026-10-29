-- want: PG009:error:2
CREATE TRIGGER orders_audit AFTER UPDATE ON public.orders FOR EACH ROW EXECUTE FUNCTION acme_audit();
