# opa test fixtures for tq.terraform: one positive and one negative per rule.
package tq.terraform_test

import data.tq.terraform
import rego.v1

plan(type, after) := {"resource_changes": [{
	"address": sprintf("%s.acme", [type]),
	"type": type,
	"change": {"actions": ["create"], "after": after},
}]}

rules(found) := {m.rule | some m in found}

# TQR001
test_tags_missing if {
	"TQR001" in rules(terraform.deny) with input as plan("aws_s3_bucket", {"tags": {"env": "dev"}, "tags_all": {"env": "dev"}})
		with data.tqconfig as {"tags": ["owner"]}
}

test_tags_present if {
	not "TQR001" in rules(terraform.deny) with input as plan("aws_s3_bucket", {"tags": null, "tags_all": {"owner": "tester"}})
		with data.tqconfig as {"tags": ["owner"]}
}

test_tags_not_required if {
	not "TQR001" in rules(terraform.deny) with input as plan("aws_s3_bucket", {"tags": {}})
}

test_deleted_resource_not_judged if {
	inp := {"resource_changes": [{"address": "aws_s3_bucket.acme", "type": "aws_s3_bucket", "change": {"actions": ["delete"], "after": null}}]}
	count(terraform.deny) == 0 with input as inp with data.tqconfig as {"tags": ["owner"]}
}

# TQR002
test_cloudtrail_off if {
	"TQR002" in rules(terraform.deny) with input as plan("aws_cloudtrail", {"enable_logging": false})
}

test_cloudtrail_on if {
	not "TQR002" in rules(terraform.deny) with input as plan("aws_cloudtrail", {"enable_logging": true})
}

test_lb_no_logs if {
	"TQR002" in rules(terraform.warn) with input as plan("aws_lb", {"access_logs": []})
}

test_lb_logs if {
	not "TQR002" in rules(terraform.warn) with input as plan("aws_lb", {"access_logs": [{"enabled": true, "bucket": "acme-logs"}]})
}

# TQR003
test_storage_old_tls if {
	"TQR003" in rules(terraform.deny) with input as plan("azurerm_storage_account", {"min_tls_version": "TLS1_0"})
}

test_storage_tls12 if {
	not "TQR003" in rules(terraform.deny) with input as plan("azurerm_storage_account", {"min_tls_version": "TLS1_2"})
}

test_postgres_tls_enforcement_disabled if {
	"TQR003" in rules(terraform.deny) with input as plan("azurerm_postgresql_server", {"ssl_minimal_tls_version_enforced": "TLSEnforcementDisabled"})
}

test_mssql_tls_disabled if {
	"TQR003" in rules(terraform.deny) with input as plan("azurerm_mssql_server", {"minimum_tls_version": "Disabled"})
}

test_mssql_tls12 if {
	not "TQR003" in rules(terraform.deny) with input as plan("azurerm_mssql_server", {"minimum_tls_version": "1.2"})
}

test_webapp_old_tls if {
	"TQR003" in rules(terraform.deny) with input as plan("azurerm_linux_web_app", {"https_only": true, "site_config": [{"minimum_tls_version": "1.1"}]})
}

test_listener_legacy_policy if {
	"TQR003" in rules(terraform.deny) with input as plan("aws_lb_listener", {"protocol": "HTTPS", "ssl_policy": "ELBSecurityPolicy-2016-08"})
}

test_listener_modern_policy if {
	not "TQR003" in rules(terraform.deny) with input as plan("aws_lb_listener", {"protocol": "HTTPS", "ssl_policy": "ELBSecurityPolicy-TLS13-1-2-2021-06"})
}

# TQR004
test_prod_short_retention if {
	"TQR004" in rules(terraform.deny) with input as plan("aws_db_instance", {"backup_retention_period": 1})
		with data.tqconfig as {"prod": true}
}

test_prod_retention_ok if {
	not "TQR004" in rules(terraform.deny) with input as plan("aws_db_instance", {"backup_retention_period": 14})
		with data.tqconfig as {"prod": true}
}

test_dev_short_retention_ok if {
	not "TQR004" in rules(terraform.deny) with input as plan("aws_db_instance", {"backup_retention_period": 1})
		with data.tqconfig as {"prod": false}
}

test_mssql_short_retention if {
	"TQR004" in rules(terraform.deny) with input as plan("azurerm_mssql_database", {"short_term_retention_policy": [{"retention_days": 3}]})
		with data.tqconfig as {"prod": true}
}

# TQR005
test_http_listener if {
	"TQR005" in rules(terraform.deny) with input as plan("aws_lb_listener", {"protocol": "HTTP", "default_action": [{"type": "forward"}]})
}

test_http_redirect_ok if {
	not "TQR005" in rules(terraform.deny) with input as plan("aws_lb_listener", {"protocol": "HTTP", "default_action": [{"type": "redirect", "redirect": [{"protocol": "HTTPS", "port": "443"}]}]})
}

test_storage_http if {
	"TQR005" in rules(terraform.deny) with input as plan("azurerm_storage_account", {"https_traffic_only_enabled": false, "min_tls_version": "TLS1_2"})
}

test_storage_https_ok if {
	not "TQR005" in rules(terraform.deny) with input as plan("azurerm_storage_account", {"https_traffic_only_enabled": true, "min_tls_version": "TLS1_2"})
}

test_webapp_http if {
	"TQR005" in rules(terraform.deny) with input as plan("azurerm_windows_web_app", {"https_only": false})
}

# TQR006
test_kms_no_rotation if {
	"TQR006" in rules(terraform.deny) with input as plan("aws_kms_key", {"enable_key_rotation": false, "customer_master_key_spec": "SYMMETRIC_DEFAULT"})
}

# An absent enable_key_rotation is off (the provider default is false).
test_kms_rotation_absent if {
	"TQR006" in rules(terraform.deny) with input as plan("aws_kms_key", {"customer_master_key_spec": "SYMMETRIC_DEFAULT"})
}

test_kms_rotation_ok if {
	not "TQR006" in rules(terraform.deny) with input as plan("aws_kms_key", {"enable_key_rotation": true, "customer_master_key_spec": "SYMMETRIC_DEFAULT"})
}

test_kms_asymmetric_ok if {
	not "TQR006" in rules(terraform.deny) with input as plan("aws_kms_key", {"enable_key_rotation": false, "customer_master_key_spec": "RSA_2048"})
}

test_vault_key_no_policy if {
	"TQR006" in rules(terraform.warn) with input as plan("azurerm_key_vault_key", {"rotation_policy": []})
}

test_vault_key_policy_ok if {
	not "TQR006" in rules(terraform.warn) with input as plan("azurerm_key_vault_key", {"rotation_policy": [{"expire_after": "P90D"}]})
}

# Every message carries addr and rule.
test_result_shape if {
	ms := terraform.deny with input as plan("aws_cloudtrail", {"enable_logging": false})
	some m in ms
	m.addr == "aws_cloudtrail.acme"
	m.rule == "TQR002"
	startswith(m.msg, "aws_cloudtrail.acme: ")
}
