select
    customer_id,
    md5(email) as email,
    phone,
    segment
from {{ source('crm', 'customers') }}
