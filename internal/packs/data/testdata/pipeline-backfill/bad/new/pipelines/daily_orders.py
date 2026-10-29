from datetime import date

from pyspark.sql import SparkSession
from pyspark.sql import functions as F

spark = SparkSession.builder.getOrCreate()

orders = spark.table("raw.orders").filter(F.col("order_date") == F.current_date())

today = date.today()
returns = spark.table("raw.returns").where(F.col("return_date") == today)

late = spark.sql(f"SELECT * FROM raw.events WHERE event_date = current_date()")

orders.write.partitionBy("order_date").mode("overwrite").saveAsTable("analytics.orders")

publish(orders, partition_date=date.today())
