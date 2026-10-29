def run(ds, spark):
    df = spark.table("staging.orders").where(f"order_date = '{ds}'").dropDuplicates(["order_id"])
    df.write.mode("append").saveAsTable("analytics.orders_log")
