-- want: PG001:warn:3 PG009:warn:3
-- opts: small=public.feature_flags
CREATE INDEX ff_idx ON public.feature_flags (name);
