{% set key = 'event_id' %}
{{ config(materialized='incremental', unique_key=key) }}
select event_id, loaded_at from {{ ref('stg_events') }}
