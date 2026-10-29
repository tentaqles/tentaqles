def run(ds, spark):
    spark.sql(f"DELETE FROM analytics.events WHERE event_date = '{ds}'")
    spark.table("staging.events").where(f"event_date = '{ds}'").write.mode("append").saveAsTable("analytics.events")
