{{ config(materialized='incremental', incremental_strategy='insert_overwrite') }}
select event_id, event_date from {{ ref('stg_events') }}
