-- tq:allow select-star: a pass-through view kept for one release
select * from {{ ref('stg_orders') }}
