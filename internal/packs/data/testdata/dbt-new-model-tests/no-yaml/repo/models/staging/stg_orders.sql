select
    id as order_id,
    customer_id
from {{ ref('raw_orders') }}
