select order_id, line_no from {{ ref('stg_lines') }}
