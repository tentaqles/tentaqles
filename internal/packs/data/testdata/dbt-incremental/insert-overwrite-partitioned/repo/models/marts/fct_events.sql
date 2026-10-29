{{ config(materialized='incremental', incremental_strategy='insert_overwrite', partition_by={'field': 'event_date', 'data_type': 'date'}) }}
select event_id, event_date from {{ ref('stg_events') }}
