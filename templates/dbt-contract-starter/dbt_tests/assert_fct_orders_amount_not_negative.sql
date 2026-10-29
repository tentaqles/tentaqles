-- A singular data test: it fails when it returns rows.
select
    order_id,
    amount
from {{ ref('fct_orders') }}
where amount < 0
