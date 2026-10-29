{{ config(materialized='incremental', incremental_strategy='microbatch', event_time='loaded_at', batch_size='day', begin='2026-01-01') }}
select event_id, loaded_at from {{ ref('stg_events') }}
