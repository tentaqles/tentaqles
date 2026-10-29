from airflow.decorators import dag, task
from airflow.utils.dates import days_ago

default_args = {"owner": "tester", "start_date": days_ago(1)}


@dag(schedule="@hourly", default_args=default_args, catchup=False)
def customers():
    @task(retries=2)
    def load():
        return 1

    load()


customers()
