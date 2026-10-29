def model(dbt, session):
    return dbt.ref('raw_customers')
