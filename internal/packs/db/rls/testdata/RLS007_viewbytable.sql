-- want: RLS007:error:2
alter table private.v set schema public;
alter table private.safe set schema public;
