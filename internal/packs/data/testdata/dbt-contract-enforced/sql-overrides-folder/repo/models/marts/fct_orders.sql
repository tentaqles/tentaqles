{{ config(contract={'enforced': false}) }}
select order_id, amount from {{ ref('stg_orders') }}
