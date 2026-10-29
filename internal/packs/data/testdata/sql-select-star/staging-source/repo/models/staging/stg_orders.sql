with source as (
    select * from {{ source('shop', 'orders') }}
)
select id as order_id from source
