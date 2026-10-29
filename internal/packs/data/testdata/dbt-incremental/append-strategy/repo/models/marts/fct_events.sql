{{ config(materialized='incremental', incremental_strategy='append') }}
select event_id, loaded_at from {{ ref('stg_events') }}
