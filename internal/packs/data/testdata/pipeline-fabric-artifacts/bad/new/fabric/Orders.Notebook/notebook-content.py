# Fabric notebook source

# METADATA ********************

# META {
# META   "kernel_info": { "name": "synapse_pyspark" },
# META   "dependencies": {
# META     "lakehouse": {
# META       "default_lakehouse": "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
# META       "default_lakehouse_name": "acme_lh",
# META       "default_lakehouse_workspace_id": "9f9f9f9f-8e8e-7d7d-6c6c-5b5b5b5b5b5b"
# META     }
# META   }
# META }

# CELL ********************

df = spark.read.parquet("abfss://9f9f9f9f-8e8e-7d7d-6c6c-5b5b5b5b5b5b@onelake.dfs.fabric.microsoft.com/acme_lh.Lakehouse/Files/orders")
df.write.mode("overwrite").saveAsTable("orders")

# METADATA ********************

# META {
# META   "language": "python",
# META   "language_group": "synapse_pyspark"
# META }
