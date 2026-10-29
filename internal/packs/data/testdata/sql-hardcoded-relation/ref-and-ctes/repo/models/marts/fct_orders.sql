with orders as (
    select order_id, customer_id from {{ ref('stg_orders') }}
),
customers as (
    select customer_id from {{ source('shop', 'customers') }}
)
select orders.order_id, extract(year from orders.order_date) as y
from orders
left join customers on customers.customer_id = orders.customer_id
