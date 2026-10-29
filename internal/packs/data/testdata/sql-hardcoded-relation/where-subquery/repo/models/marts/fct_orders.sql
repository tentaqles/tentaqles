select id
from {{ ref('stg_orders') }}
where id not in (
  select id from raw.blocked_orders
)
