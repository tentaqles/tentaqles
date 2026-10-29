from pyspark.sql import functions as F


def run(spark):
    customers = spark.table("crm.customers")
    out = customers.select("customer_id", F.col("email"), "segment")
    out.write.mode("overwrite").saveAsTable("analytics.customer_export")
