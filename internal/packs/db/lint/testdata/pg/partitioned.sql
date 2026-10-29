-- want: PG001:warn:3
SET lock_timeout = '5s';
CREATE INDEX events_at_idx ON events (at);
