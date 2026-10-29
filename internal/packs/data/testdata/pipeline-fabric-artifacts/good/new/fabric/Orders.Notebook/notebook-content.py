# Fabric notebook source

# METADATA ********************

# META {
# META   "kernel_info": { "name": "synapse_pyspark" },
# META   "dependencies": {
# META     "lakehouse": {
# META       "default_lakehouse_name": "acme_lh",
# META       "default_lakehouse_workspace_id": "00000000-0000-0000-0000-000000000000"
# META     }
# META   }
# META }

# PARAMETERS CELL ********************

run_date = "2026-01-01"

# CELL ********************

df = spark.read.parquet(f"Files/orders/{run_date}")
df.write.mode("overwrite").option("replaceWhere", f"order_date = '{run_date}'").saveAsTable("orders")

# METADATA ********************

# META {
# META   "language": "python",
# META   "language_group": "synapse_pyspark"
# META }
