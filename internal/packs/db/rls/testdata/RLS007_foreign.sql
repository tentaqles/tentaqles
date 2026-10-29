-- want: RLS007:error:2 RLS007:error:4 RLS007:error:6 RLS007:error:7 RLS007:error:8
create foreign table public.ft (id int) server remote;
create foreign table if not exists private.ft2 (id int) server remote;
alter foreign table private.ft2 set schema public;
create foreign table private.ft3 (id int) server remote;
alter table private.ft3 set schema public;
alter foreign table if exists private.unseen set schema public;
import foreign schema remote_api limit to (orders) from server remote into public;
import foreign schema remote_api from server remote into private;
alter foreign table private.ft4 rename to ft5;
drop foreign table if exists private.ft5;
