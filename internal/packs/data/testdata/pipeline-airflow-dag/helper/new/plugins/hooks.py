from airflow.hooks.base import BaseHook


def conn(name):
    return BaseHook.get_connection(name)
