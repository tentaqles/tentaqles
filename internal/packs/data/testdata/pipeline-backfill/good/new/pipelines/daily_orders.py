import sys

from pyspark.sql import SparkSession
from pyspark.sql import functions as F

spark = SparkSession.builder.getOrCreate()
ds = sys.argv[1]

orders = (
    spark.table("raw.orders")
    .filter(F.col("order_date") == F.lit(ds))
    .withColumn("loaded_at", F.current_timestamp())
)
orders.write.option("replaceWhere", f"order_date = '{ds}'").mode("overwrite").saveAsTable("analytics.orders")
