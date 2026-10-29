select o.id
from {{ ref('stg_orders') }} as o
where exists (select 1 from acme_db.raw.refunds r where r.order_id = o.id)
