select count(*) as n, 2 * 3 as six from {{ ref('stg_orders') }}
