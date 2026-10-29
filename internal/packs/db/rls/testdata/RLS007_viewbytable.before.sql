create view private.v as select 1;
create view private.safe with (security_invoker = true) as select 1;
