import pendulum
from airflow import DAG

# tq:allow airflow-catchup-unset: Airflow 3 default (False) is intended here
with DAG(dag_id="legacy", start_date=pendulum.datetime(2026, 1, 1, tz="UTC"), schedule=None, default_args={"retries": 1}) as dag:
    pass
