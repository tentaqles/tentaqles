{{ config(
    materialized='incremental',
    unique_key='order_id',
    on_schema_change='append_new_columns'
) }}

-- The orders fact behind contracts/fct_orders.odcs.yaml. A rerun of the
-- same interval merges on order_id, and the 3-day lookback picks up late
-- updates.
select
    o.order_id,
    o.customer_id,
    o.order_date,
    o.status,
    cast(o.amount as numeric(12, 2)) as amount,
    o.updated_at
from {{ ref('stg_orders') }} as o
inner join {{ ref('stg_customers') }} as c
    on o.customer_id = c.customer_id
{% if is_incremental() %}
    where o.updated_at > (select max(t.updated_at) - interval '3 days' from {{ this }} as t)
{% endif %}
