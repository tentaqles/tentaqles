{{ config(materialized='incremental', unique_key='event_id') }}
select event_id, loaded_at from {{ ref('stg_events') }}
{% if is_incremental() %}
where loaded_at > (select max(loaded_at) from {{ this }})
{% endif %}
