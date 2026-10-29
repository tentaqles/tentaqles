select o.order_id
from analytics.orders o
join {{ ref('stg_customers') }} c on c.customer_id = o.customer_id
