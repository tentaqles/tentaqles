select order_id, amount
from analytics.orders
where order_date = current_date
