import pandas as pd
from sqlalchemy import create_engine


def run(ds, spark):
    df = spark.table("staging.orders").where(f"order_date = '{ds}'")
    df.write.mode("append").saveAsTable("analytics.orders")
    pdf = pd.read_parquet(f"exports/{ds}.parquet")
    pdf.to_sql("orders_copy", create_engine("sqlite://"), if_exists="append")
    spark.sql("""
        INSERT INTO analytics.order_lines
        SELECT * FROM staging.order_lines
    """)
