{{ config(materialized='incremental', unique_key=var('event_key')) }}
select event_id, loaded_at from {{ ref('stg_events') }}
