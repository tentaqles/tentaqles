# tq.arm: rules over ARM resources, from ARM templates (bicep build output)
# and what-if payloads (spec D §7.4). tq feeds
#   {"resources": [{"tq_addr", "type", "name", "properties", "tags", ...}]}
# with nested resources flattened. Written fresh for tentaqles; Rego v1.
package tq.arm

import data.tq.lib
import rego.v1

resources contains r if {
	some r in input.resources
	is_object(r)
}

kind(r) := lower(r.type)

addr(r) := r.tq_addr if {
	is_string(r.tq_addr)
} else := r.name

props(r) := r.properties if {
	is_object(r.properties)
} else := {}

# Resource types that take tags and are commonly governed by tag policy.
taggable(r) if lib.has_key(r, "tags")

taggable(r) if {
	not startswith(kind(r), "microsoft.resources/deployments")
	count(split(kind(r), "/")) == 2
	not kind(r) in {"microsoft.authorization/roleassignments", "microsoft.authorization/roledefinitions", "microsoft.insights/diagnosticsettings", "microsoft.authorization/locks"}
}

tags(r) := r.tags if {
	is_object(r.tags)
} else := {}

# TQR001: required tags.
deny contains lib.result("TQR001", addr(r), sprintf("missing required tags: %s", [concat(", ", sort(missing))])) if {
	some r in resources
	count(lib.required_tags) > 0
	taggable(r)
	not is_string(r.tags) # an ARM expression ("[parameters('tags')]") is not judged
	missing := lib.missing_tags(tags(r))
	count(missing) > 0
}

# TQR002: diagnostic logging.
deny contains lib.result("TQR002", addr(r), "the diagnostic setting enables no log category") if {
	some r in resources
	kind(r) == "microsoft.insights/diagnosticsettings"
	logs := props(r).logs
	count(logs) > 0
	not any_log_on(logs)
}

any_log_on(logs) if {
	some l in logs
	lib.is_true(l.enabled)
}

# TQR003: TLS 1.2 minimum.
tls_prop := {
	"microsoft.storage/storageaccounts": "minimumTlsVersion",
	"microsoft.sql/servers": "minimalTlsVersion",
	"microsoft.cache/redis": "minimumTlsVersion",
	"microsoft.dbforpostgresql/servers": "minimalTlsVersion",
}

deny contains lib.result("TQR003", addr(r), sprintf("minimum TLS version %v is below 1.2", [v])) if {
	some r in resources
	p := tls_prop[kind(r)]
	v := props(r)[p]
	v in lib.old_tls
}

deny contains lib.result("TQR003", addr(r), sprintf("minimum TLS version %v is below 1.2", [v])) if {
	some r in resources
	kind(r) == "microsoft.web/sites"
	v := props(r).siteConfig.minTlsVersion
	v in lib.old_tls
}

# TQR004: backup retention on production databases.
deny contains lib.result("TQR004", addr(r), sprintf("backup retention is %v days, below %v on a production database", [d, lib.min_retention_days])) if {
	lib.prod
	some r in resources
	kind(r) in {"microsoft.dbforpostgresql/flexibleservers", "microsoft.dbformysql/flexibleservers"}
	d := lib.num(props(r).backup.backupRetentionDays)
	d < lib.min_retention_days
}

deny contains lib.result("TQR004", addr(r), sprintf("short-term backup retention is %v days, below %v on a production database", [d, lib.min_retention_days])) if {
	lib.prod
	some r in resources
	kind(r) == "microsoft.sql/servers/databases/backupshorttermretentionpolicies"
	d := lib.num(props(r).retentionDays)
	d < lib.min_retention_days
}

# TQR005: no plain-HTTP listeners.
deny contains lib.result("TQR005", addr(r), "the storage account accepts plain HTTP (supportsHttpsTrafficOnly is false)") if {
	some r in resources
	kind(r) == "microsoft.storage/storageaccounts"
	lib.is_false(props(r).supportsHttpsTrafficOnly)
}

deny contains lib.result("TQR005", addr(r), "the app accepts plain HTTP (httpsOnly is not true)") if {
	some r in resources
	kind(r) == "microsoft.web/sites"
	not lib.is_true(object.get(props(r), "httpsOnly", null))
}

# TQR006: key rotation.
warn contains lib.result("TQR006", addr(r), "the key has no rotation policy") if {
	some r in resources
	kind(r) == "microsoft.keyvault/vaults/keys"
	not props(r).rotationPolicy
}
