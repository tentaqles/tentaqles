with orders as (
  select id, extract(year from o.created_at) as yr, substring(o.code from 2 for 3) as c
  from {{ ref('stg_orders') }} o
)
select id, yr, c from orders
where id in (select id from {{ ref('stg_kept') }})
  and id in (select id from {{ this }})
