# tq.terraform: rules over Terraform/OpenTofu plan JSON (spec D §7.4).
# tq feeds the redacted plan as
#   {"resource_changes": [{"address", "type", "change": {"actions", "after"}}]}
# Written fresh for tentaqles; Rego v1.
package tq.terraform

import data.tq.lib
import rego.v1

# changes are the resources the plan creates or changes.
changes contains rc if {
	some rc in input.resource_changes
	some a in rc.change.actions
	a in {"create", "update", "replace", "unknown"}
	is_object(rc.change.after)
}

tag_map(after) := after.tags_all if {
	is_object(after.tags_all)
} else := after.tags if {
	is_object(after.tags)
} else := {}

# TQR001: required tags.
deny contains lib.result("TQR001", rc.address, sprintf("missing required tags: %s", [concat(", ", sort(missing))])) if {
	some rc in changes
	lib.has_key(rc.change.after, "tags")
	missing := lib.missing_tags(tag_map(rc.change.after))
	count(missing) > 0
}

# TQR002: diagnostic logging.
deny contains lib.result("TQR002", rc.address, "CloudTrail logging is switched off") if {
	some rc in changes
	rc.type == "aws_cloudtrail"
	lib.is_false(rc.change.after.enable_logging)
}

warn contains lib.result("TQR002", rc.address, "the load balancer has no access logs") if {
	some rc in changes
	rc.type in {"aws_lb", "aws_alb"}
	not lb_logs_on(rc.change.after)
}

lb_logs_on(after) if {
	some al in after.access_logs
	lib.is_true(al.enabled)
}

# TQR003: TLS 1.2 minimum.
tls_attr := {
	"azurerm_storage_account": "min_tls_version",
	"azurerm_mssql_server": "minimum_tls_version",
	"azurerm_redis_cache": "minimum_tls_version",
	"azurerm_postgresql_server": "ssl_minimal_tls_version_enforced",
}

deny contains lib.result("TQR003", rc.address, sprintf("minimum TLS version %v is below 1.2", [v])) if {
	some rc in changes
	attr := tls_attr[rc.type]
	v := rc.change.after[attr]
	v in lib.old_tls
}

deny contains lib.result("TQR003", rc.address, sprintf("minimum TLS version %v is below 1.2", [v])) if {
	some rc in changes
	rc.type in {"azurerm_linux_web_app", "azurerm_windows_web_app", "azurerm_linux_function_app", "azurerm_windows_function_app"}
	some sc in rc.change.after.site_config
	v := sc.minimum_tls_version
	v in lib.old_tls
}

deny contains lib.result("TQR003", rc.address, sprintf("SSL policy %v allows TLS below 1.2", [p])) if {
	some rc in changes
	rc.type in {"aws_lb_listener", "aws_alb_listener"}
	p := rc.change.after.ssl_policy
	p in lib.legacy_ssl_policies
}

# TQR004: backup retention on production databases.
deny contains lib.result("TQR004", rc.address, sprintf("backup retention is %v days, below %v on a production database", [d, lib.min_retention_days])) if {
	lib.prod
	some rc in changes
	attr := {"aws_db_instance": "backup_retention_period", "aws_rds_cluster": "backup_retention_period", "azurerm_postgresql_flexible_server": "backup_retention_days", "azurerm_mysql_flexible_server": "backup_retention_days"}[rc.type]
	d := lib.num(rc.change.after[attr])
	d < lib.min_retention_days
}

deny contains lib.result("TQR004", rc.address, sprintf("short-term backup retention is %v days, below %v on a production database", [d, lib.min_retention_days])) if {
	lib.prod
	some rc in changes
	rc.type == "azurerm_mssql_database"
	some p in rc.change.after.short_term_retention_policy
	d := lib.num(p.retention_days)
	d < lib.min_retention_days
}

# TQR005: no plain-HTTP listeners.
deny contains lib.result("TQR005", rc.address, "an HTTP listener that does not redirect to HTTPS") if {
	some rc in changes
	rc.type in {"aws_lb_listener", "aws_alb_listener"}
	rc.change.after.protocol == "HTTP"
	not redirects_https(rc.change.after)
}

redirects_https(after) if {
	some da in after.default_action
	da.type == "redirect"
	some r in da.redirect
	r.protocol == "HTTPS"
}

deny contains lib.result("TQR005", rc.address, "the storage account accepts plain HTTP") if {
	some rc in changes
	rc.type == "azurerm_storage_account"
	some attr in ["https_traffic_only_enabled", "enable_https_traffic_only"]
	lib.is_false(rc.change.after[attr])
}

deny contains lib.result("TQR005", rc.address, "the app accepts plain HTTP (https_only is false)") if {
	some rc in changes
	rc.type in {"azurerm_linux_web_app", "azurerm_windows_web_app", "azurerm_linux_function_app", "azurerm_windows_function_app", "azurerm_app_service"}
	lib.is_false(rc.change.after.https_only)
}

# TQR006: key rotation.
deny contains lib.result("TQR006", rc.address, "automatic key rotation is off on a symmetric KMS key") if {
	some rc in changes
	rc.type == "aws_kms_key"
	symmetric(rc.change.after)
	not lib.is_true(object.get(rc.change.after, "enable_key_rotation", null))
}

symmetric(after) if not lib.has_key(after, "customer_master_key_spec")

symmetric(after) if after.customer_master_key_spec == null

symmetric(after) if after.customer_master_key_spec == "SYMMETRIC_DEFAULT"

warn contains lib.result("TQR006", rc.address, "the key has no rotation policy") if {
	some rc in changes
	rc.type == "azurerm_key_vault_key"
	not has_rotation(rc.change.after)
}

has_rotation(after) if count(after.rotation_policy) > 0
