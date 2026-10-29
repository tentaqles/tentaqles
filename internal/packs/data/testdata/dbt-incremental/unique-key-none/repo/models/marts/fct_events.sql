{{ config(materialized='incremental', unique_key=none) }}
select event_id, loaded_at from {{ ref('stg_events') }}
