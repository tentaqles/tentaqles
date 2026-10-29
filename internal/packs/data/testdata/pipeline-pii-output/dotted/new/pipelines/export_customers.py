import acme
from pyspark.sql import functions as F
from acme import udfs


def run(spark):
    customers = spark.table("crm.customers")
    out = customers.select("customer_id", acme.mask_email(F.col("email")).alias("email_masked"))
    out.write.mode("overwrite").saveAsTable("analytics.customer_export")
    out2 = customers.select("customer_id", udfs.acme.mask_email(F.col("email")).alias("email_masked"))
    out2.write.mode("overwrite").saveAsTable("analytics.customer_export2")
