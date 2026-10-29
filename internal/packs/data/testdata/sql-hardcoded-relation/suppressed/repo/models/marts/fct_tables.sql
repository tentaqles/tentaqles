select table_name
{# tq:allow data/sql-hardcoded-relation: catalog metadata, not a model input #}
from information_schema.tables
