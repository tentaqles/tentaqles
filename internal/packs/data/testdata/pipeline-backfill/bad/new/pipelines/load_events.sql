-- Loads yesterday's events
INSERT OVERWRITE TABLE analytics.events
SELECT event_id, event_date
FROM raw.events
WHERE event_date = CURRENT_DATE - INTERVAL '1' DAY;
