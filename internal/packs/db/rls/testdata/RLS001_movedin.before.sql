create table private.secrets (id bigint primary key);
create table private.guarded (id bigint primary key);
alter table private.guarded enable row level security;
create policy own on private.guarded for select to authenticated using (id > 0);
create table private.stays (id bigint);
