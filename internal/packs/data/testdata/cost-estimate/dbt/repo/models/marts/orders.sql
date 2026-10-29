select status, count(*) as n
from {{ ref('stg_orders') }}
group by status
