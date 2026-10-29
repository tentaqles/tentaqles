-- want: RLS001:error:2
alter table private.secrets set schema public;
alter table private.guarded set schema public;
alter table private.stays set schema internal;
