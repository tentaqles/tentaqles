CREATE TABLE events (id bigint, at timestamptz) PARTITION BY RANGE (at);
