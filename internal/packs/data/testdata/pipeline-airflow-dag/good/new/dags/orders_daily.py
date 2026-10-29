import pendulum
from airflow import DAG
from airflow.operators.python import PythonOperator

default_args = {"owner": "tester", "retries": 3}


def load(ds, **_):
    import pandas as pd

    return pd.read_csv(f"exports/{ds}.csv")


with DAG(
    dag_id="orders_daily",
    start_date=pendulum.datetime(2026, 1, 1, tz="UTC"),
    schedule="@daily",
    catchup=False,
    default_args=default_args,
) as dag:
    PythonOperator(task_id="load", python_callable=load)
