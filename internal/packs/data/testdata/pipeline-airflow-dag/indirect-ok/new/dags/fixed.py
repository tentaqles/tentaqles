import datetime
import pendulum
from airflow import DAG

START = pendulum.datetime(2026, 1, 1, tz="UTC")
RUN_STAMP = datetime.datetime.now()  # logged, not a start date
default_args = {"owner": "tester", "start_date": START, "retries": 1}

with DAG("orders_fixed", default_args=default_args, catchup=False) as dag:
    print(RUN_STAMP)
