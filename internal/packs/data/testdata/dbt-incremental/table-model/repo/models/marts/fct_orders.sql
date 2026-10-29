{{ config(materialized='table') }}
select order_id from {{ ref('stg_orders') }}
