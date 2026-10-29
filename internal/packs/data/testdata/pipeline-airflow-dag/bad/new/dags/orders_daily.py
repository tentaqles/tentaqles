from datetime import datetime

import pandas as pd
import requests
from airflow import DAG
from airflow.operators.python import PythonOperator

lookup = pd.read_csv("lookup.csv")
token = requests.get("https://example.test/token")

with DAG(
    dag_id="orders_daily",
    start_date=datetime.now(),
    schedule="@daily",
) as dag:
    PythonOperator(task_id="load", python_callable=print)
