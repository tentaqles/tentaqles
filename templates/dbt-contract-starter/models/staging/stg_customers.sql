-- One row per customer. Names and emails stay in the seed: no personal data
-- leaves the staging layer.
select
    customer_id,
    cast(created_at as date) as signup_date
from {{ ref('raw_customers') }}
