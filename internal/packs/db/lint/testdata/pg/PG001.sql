-- want: PG001:error:3
SET lock_timeout = '5s';
CREATE INDEX orders_status_idx ON public.orders (status);
