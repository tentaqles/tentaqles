{{ config(materialized='incremental') }}
select event_id, loaded_at from {{ ref('stg_events') }}
{% if is_incremental() %}
where loaded_at > (select max(loaded_at) - interval '3 days' from {{ this }})
{% endif %}
