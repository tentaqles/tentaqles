# Shared helpers of the tq IaC bundle (spec D §7.4). Written fresh for
# tentaqles; Rego v1.
#
# tq writes the run's settings to a data file read as data.tqconfig:
#   tags: the required tag names (packs.cloud.tags)
#   prod: whether the input targets a production environment
package tq.lib

import rego.v1

default required_tags := []

required_tags := data.tqconfig.tags

default prod := false

prod := data.tqconfig.prod

# result is one finding: tq reads msg, and the addr and rule keys, which
# Conftest moves into the result's metadata.
result(rule, addr, msg) := {"msg": sprintf("%s: %s", [addr, msg]), "addr": addr, "rule": rule}

# missing_tags returns the required tags absent from tags (a map).
missing_tags(tags) := {t |
	some t in required_tags
	not has_key(tags, t)
}

has_key(obj, k) if {
	is_object(obj)
	k in object.keys(obj)
}

# Booleans may arrive as strings (CloudFormation).
is_true(x) if x == true

is_true(x) if {
	is_string(x)
	lower(x) == "true"
}

is_false(x) if x == false

is_false(x) if {
	is_string(x)
	lower(x) == "false"
}

# num reads a number or a numeric string.
num(x) := x if is_number(x)

num(x) := to_number(x) if is_string(x)

# Legacy TLS versions, in the spellings the providers use.
old_tls := {"1.0", "1.1", "TLS1_0", "TLS1_1", "tls1_0", "tls1_1", "TLSv1", "TLSv1.1", "Tls10", "Tls11"} | no_tls_minimum

# Values that turn the TLS minimum off altogether: azurerm_postgresql_server
# ssl_minimal_tls_version_enforced, azurerm_mssql_server minimum_tls_version,
# and the ARM minimalTlsVersion.
no_tls_minimum := {"TLSEnforcementDisabled", "Disabled", "None"}

# AWS load balancer security policies that allow TLS 1.0 or 1.1.
legacy_ssl_policies := {
	"ELBSecurityPolicy-2015-05",
	"ELBSecurityPolicy-2016-08",
	"ELBSecurityPolicy-TLS-1-0-2015-04",
	"ELBSecurityPolicy-TLS-1-1-2017-01",
	"ELBSecurityPolicy-FS-2018-06",
	"ELBSecurityPolicy-FS-1-1-2019-08",
	"ELBSecurityPolicy-TLS13-1-0-2021-06",
	"ELBSecurityPolicy-TLS13-1-1-2021-06",
}

min_retention_days := 7
