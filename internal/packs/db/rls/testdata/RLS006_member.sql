-- want: RLS006:error:2 RLS006:error:3 RLS006:error:4
grant authenticated to anon;
grant service_role to public;
grant authenticated, app_admin to anon with admin option;
grant anon to authenticator;
grant app_reader to authenticated;
