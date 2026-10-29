select order_date, sum(amount) as total
from {{ ref('stg_orders') }}
group by order_date
