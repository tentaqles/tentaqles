import datetime
from airflow import DAG

YESTERDAY = datetime.datetime.now() - datetime.timedelta(days=1)
START = YESTERDAY.replace(hour=0)
default_args = {"owner": "tester", "start_date": YESTERDAY, "retries": 1}

with DAG("orders_composer", default_args=default_args, catchup=False) as dag:
    pass

with DAG("orders_chained", start_date=START, catchup=False, default_args=default_args) as dag2:
    pass
