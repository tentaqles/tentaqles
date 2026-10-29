-- One row per order, amounts converted from cents.
select
    order_id,
    customer_id,
    cast(order_date as date) as order_date,
    status,
    amount_cents / 100.0 as amount,
    cast(updated_at as timestamp) as updated_at
from {{ ref('raw_orders') }}
