from pyspark.sql import functions as F


def run(spark):
    customers = spark.table("crm.customers")
    out = customers.select("customer_id", F.sha2(F.col("email"), 256).alias("email_hash"), "segment")
    out.write.mode("overwrite").saveAsTable("analytics.customer_export")
