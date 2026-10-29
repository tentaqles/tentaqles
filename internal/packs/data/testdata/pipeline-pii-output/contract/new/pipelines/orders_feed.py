def run(spark):
    orders = spark.sql("SELECT order_id, buyer_cpf FROM analytics.orders")
    orders.write.mode("overwrite").parquet("exports/orders_feed")
