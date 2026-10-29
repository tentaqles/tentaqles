with o as (
    select order_id, amount from {{ ref('stg_orders') }}
)
select o.* , 1 as n from o
