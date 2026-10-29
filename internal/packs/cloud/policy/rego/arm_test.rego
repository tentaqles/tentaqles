# opa test fixtures for tq.arm: one positive and one negative per rule.
package tq.arm_test

import data.tq.arm
import rego.v1

doc(type, props) := {"resources": [{"tq_addr": "acme", "type": type, "name": "acme", "properties": props}]}

tagged(type, tags) := {"resources": [{"tq_addr": "acme", "type": type, "name": "acme", "tags": tags, "properties": {}}]}

rules(found) := {m.rule | some m in found}

# TQR001
test_tags_missing if {
	"TQR001" in rules(arm.deny) with input as tagged("Microsoft.Storage/storageAccounts", {"env": "dev"})
		with data.tqconfig as {"tags": ["owner"]}
}

test_tags_present if {
	not "TQR001" in rules(arm.deny) with input as tagged("Microsoft.Storage/storageAccounts", {"owner": "tester"})
		with data.tqconfig as {"tags": ["owner"]}
}

test_tags_absent if {
	"TQR001" in rules(arm.deny) with input as doc("Microsoft.Storage/storageAccounts", {})
		with data.tqconfig as {"tags": ["owner"]}
}

test_tags_expression_not_judged if {
	not "TQR001" in rules(arm.deny) with input as tagged("Microsoft.Storage/storageAccounts", "[parameters('tags')]")
		with data.tqconfig as {"tags": ["owner"]}
}

test_role_assignment_not_taggable if {
	not "TQR001" in rules(arm.deny) with input as doc("Microsoft.Authorization/roleAssignments", {})
		with data.tqconfig as {"tags": ["owner"]}
}

# TQR002
test_diag_all_off if {
	"TQR002" in rules(arm.deny) with input as doc("Microsoft.Insights/diagnosticSettings", {"logs": [{"category": "AuditEvent", "enabled": false}]})
}

test_diag_on if {
	not "TQR002" in rules(arm.deny) with input as doc("Microsoft.Insights/diagnosticSettings", {"logs": [{"category": "AuditEvent", "enabled": true}]})
}

# TQR003
test_storage_old_tls if {
	"TQR003" in rules(arm.deny) with input as doc("Microsoft.Storage/storageAccounts", {"minimumTlsVersion": "TLS1_1", "supportsHttpsTrafficOnly": true})
}

test_storage_tls12 if {
	not "TQR003" in rules(arm.deny) with input as doc("Microsoft.Storage/storageAccounts", {"minimumTlsVersion": "TLS1_2", "supportsHttpsTrafficOnly": true})
}

test_sql_old_tls if {
	"TQR003" in rules(arm.deny) with input as doc("Microsoft.Sql/servers", {"minimalTlsVersion": "1.0"})
}

test_sql_tls_none if {
	"TQR003" in rules(arm.deny) with input as doc("Microsoft.Sql/servers", {"minimalTlsVersion": "None"})
}

test_sql_tls12 if {
	not "TQR003" in rules(arm.deny) with input as doc("Microsoft.Sql/servers", {"minimalTlsVersion": "1.2"})
}

test_site_old_tls if {
	"TQR003" in rules(arm.deny) with input as doc("Microsoft.Web/sites", {"httpsOnly": true, "siteConfig": {"minTlsVersion": "1.0"}})
}

# TQR004
test_pg_short_retention if {
	"TQR004" in rules(arm.deny) with input as doc("Microsoft.DBforPostgreSQL/flexibleServers", {"backup": {"backupRetentionDays": 3}})
		with data.tqconfig as {"prod": true}
}

test_pg_retention_ok if {
	not "TQR004" in rules(arm.deny) with input as doc("Microsoft.DBforPostgreSQL/flexibleServers", {"backup": {"backupRetentionDays": 7}})
		with data.tqconfig as {"prod": true}
}

test_pg_short_retention_dev_ok if {
	not "TQR004" in rules(arm.deny) with input as doc("Microsoft.DBforPostgreSQL/flexibleServers", {"backup": {"backupRetentionDays": 3}})
}

# TQR005
test_storage_http if {
	"TQR005" in rules(arm.deny) with input as doc("Microsoft.Storage/storageAccounts", {"supportsHttpsTrafficOnly": false})
}

test_storage_https_ok if {
	not "TQR005" in rules(arm.deny) with input as doc("Microsoft.Storage/storageAccounts", {"supportsHttpsTrafficOnly": true})
}

test_site_http if {
	"TQR005" in rules(arm.deny) with input as doc("Microsoft.Web/sites", {})
}

test_site_https_ok if {
	not "TQR005" in rules(arm.deny) with input as doc("Microsoft.Web/sites", {"httpsOnly": true})
}

# TQR006
test_key_no_rotation if {
	"TQR006" in rules(arm.warn) with input as doc("Microsoft.KeyVault/vaults/keys", {"kty": "RSA"})
}

test_key_rotation_ok if {
	not "TQR006" in rules(arm.warn) with input as doc("Microsoft.KeyVault/vaults/keys", {"kty": "RSA", "rotationPolicy": {"lifetimeActions": []}})
}
