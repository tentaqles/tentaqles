from delta.tables import DeltaTable


def run(ds, spark):
    src = spark.table("staging.orders").where(f"order_date = '{ds}'")
    tgt = DeltaTable.forName(spark, "analytics.orders")
    tgt.alias("t").merge(src.alias("s"), "t.order_id = s.order_id").whenNotMatchedInsertAll().execute()
    src.write.mode("append").saveAsTable("analytics.orders_audit")
