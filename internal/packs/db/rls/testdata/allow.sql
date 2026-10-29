-- want: RLS007:error:5 SUP001:error:4
-- tq:allow RLS007: static lookup data, no tenant rows
create view public.countries as select 1 as id;
-- tq:allow RLS007
create view public.regions as select 1 as id;
