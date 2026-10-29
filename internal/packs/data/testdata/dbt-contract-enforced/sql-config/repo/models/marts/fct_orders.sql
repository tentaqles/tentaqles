{{ config(materialized='table', contract={'enforced': true}) }}
select order_id, amount from {{ ref('stg_orders') }}
