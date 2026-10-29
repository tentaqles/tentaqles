# opa test fixtures for tq.cfn: one positive and one negative per rule.
package tq.cfn_test

import data.tq.cfn
import rego.v1

tpl(type, props) := {"Resources": {"AcmeRes": {"Type": type, "Properties": props}}}

rules(found) := {m.rule | some m in found}

# TQR001
test_tags_missing if {
	"TQR001" in rules(cfn.deny) with input as tpl("AWS::S3::Bucket", {"Tags": [{"Key": "env", "Value": "dev"}]})
		with data.tqconfig as {"tags": ["owner"]}
}

test_tags_absent_on_taggable if {
	"TQR001" in rules(cfn.deny) with input as tpl("AWS::S3::Bucket", {})
		with data.tqconfig as {"tags": ["owner"]}
}

test_tags_present if {
	not "TQR001" in rules(cfn.deny) with input as tpl("AWS::S3::Bucket", {"Tags": [{"Key": "owner", "Value": "tester"}]})
		with data.tqconfig as {"tags": ["owner"]}
}

test_tags_intrinsic_not_judged if {
	not "TQR001" in rules(cfn.deny) with input as tpl("AWS::S3::Bucket", {"Tags": {"Fn::If": ["IsProd", [], []]}})
		with data.tqconfig as {"tags": ["owner"]}
}

# TQR002
test_trail_off if {
	"TQR002" in rules(cfn.deny) with input as tpl("AWS::CloudTrail::Trail", {"IsLogging": "false"})
}

test_trail_on if {
	not "TQR002" in rules(cfn.deny) with input as tpl("AWS::CloudTrail::Trail", {"IsLogging": true})
}

test_lb_no_logs if {
	"TQR002" in rules(cfn.warn) with input as tpl("AWS::ElasticLoadBalancingV2::LoadBalancer", {})
}

test_lb_logs if {
	not "TQR002" in rules(cfn.warn) with input as tpl("AWS::ElasticLoadBalancingV2::LoadBalancer", {"LoadBalancerAttributes": [{"Key": "access_logs.s3.enabled", "Value": "true"}]})
}

# TQR003
test_listener_legacy if {
	"TQR003" in rules(cfn.deny) with input as tpl("AWS::ElasticLoadBalancingV2::Listener", {"Protocol": "HTTPS", "SslPolicy": "ELBSecurityPolicy-TLS-1-1-2017-01"})
}

test_listener_modern if {
	not "TQR003" in rules(cfn.deny) with input as tpl("AWS::ElasticLoadBalancingV2::Listener", {"Protocol": "HTTPS", "SslPolicy": "ELBSecurityPolicy-TLS13-1-2-2021-06"})
}

# TQR004
test_rds_short_retention if {
	"TQR004" in rules(cfn.deny) with input as tpl("AWS::RDS::DBInstance", {"BackupRetentionPeriod": "1"})
		with data.tqconfig as {"prod": true}
}

test_rds_retention_ok if {
	not "TQR004" in rules(cfn.deny) with input as tpl("AWS::RDS::DBInstance", {"BackupRetentionPeriod": 7})
		with data.tqconfig as {"prod": true}
}

# TQR005
test_http_listener if {
	"TQR005" in rules(cfn.deny) with input as tpl("AWS::ElasticLoadBalancingV2::Listener", {"Protocol": "HTTP", "DefaultActions": [{"Type": "forward"}]})
}

test_http_redirect_ok if {
	not "TQR005" in rules(cfn.deny) with input as tpl("AWS::ElasticLoadBalancingV2::Listener", {"Protocol": "HTTP", "DefaultActions": [{"Type": "redirect", "RedirectConfig": {"Protocol": "HTTPS"}}]})
}

# TQR006
test_kms_no_rotation if {
	"TQR006" in rules(cfn.deny) with input as tpl("AWS::KMS::Key", {})
}

test_kms_rotation_ok if {
	not "TQR006" in rules(cfn.deny) with input as tpl("AWS::KMS::Key", {"EnableKeyRotation": true})
}

test_kms_asymmetric_ok if {
	not "TQR006" in rules(cfn.deny) with input as tpl("AWS::KMS::Key", {"KeySpec": "RSA_2048"})
}
