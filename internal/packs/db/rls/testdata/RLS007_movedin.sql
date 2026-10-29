-- want: RLS007:error:2
alter view private.v set schema public;
alter view private.safe set schema public;
