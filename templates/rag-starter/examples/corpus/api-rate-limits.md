---
title: API rate limits
document_type: reference
---
The public API allows 600 requests per minute per workspace. Requests over the limit receive HTTP 429 with a Retry-After header giving the number of seconds to wait.

Use exponential backoff with jitter when retrying. Bulk endpoints accept up to 500 records per call and count as a single request.
