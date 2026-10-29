select o.id
from {{ ref('stg_orders') }} o
join {{ var('db') }}.raw.customers c on c.id = o.customer_id
