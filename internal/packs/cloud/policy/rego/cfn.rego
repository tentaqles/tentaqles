# tq.cfn: rules over CloudFormation templates, CDK-synthesised templates and
# change sets (spec D §7.4). tq feeds
#   {"Resources": {"<LogicalId>": {"Type", "Properties"}}}
# with YAML short tags in their long form. Written fresh for tentaqles;
# Rego v1.
package tq.cfn

import data.tq.lib
import rego.v1

props(r) := r.Properties if {
	is_object(r.Properties)
} else := {}

# Tags in CloudFormation are a [{Key, Value}] list.
tag_keys(r) := {t.Key | some t in props(r).Tags}

# Types whose Tags property is a [{Key, Value}] list.
taggable(r) if is_array(props(r).Tags)

taggable(r) if r.Type in {
	"AWS::S3::Bucket", "AWS::RDS::DBInstance", "AWS::RDS::DBCluster", "AWS::DynamoDB::Table",
	"AWS::EC2::Instance", "AWS::EC2::Volume", "AWS::EC2::VPC", "AWS::EC2::SecurityGroup",
	"AWS::KMS::Key", "AWS::Lambda::Function", "AWS::ElasticLoadBalancingV2::LoadBalancer",
	"AWS::SQS::Queue", "AWS::SNS::Topic", "AWS::IAM::Role",
}

# TQR001: required tags.
deny contains lib.result("TQR001", id, sprintf("missing required tags: %s", [concat(", ", sort(missing))])) if {
	some id, r in input.Resources
	count(lib.required_tags) > 0
	taggable(r)
	not is_object(props(r).Tags) # an intrinsic (Fn::If, Ref) is not judged
	missing := {t | some t in lib.required_tags; not t in tag_keys(r)}
	count(missing) > 0
}

# TQR002: diagnostic logging.
deny contains lib.result("TQR002", id, "CloudTrail logging is switched off (IsLogging is false)") if {
	some id, r in input.Resources
	r.Type == "AWS::CloudTrail::Trail"
	lib.is_false(props(r).IsLogging)
}

warn contains lib.result("TQR002", id, "the load balancer has no access logs") if {
	some id, r in input.Resources
	r.Type == "AWS::ElasticLoadBalancingV2::LoadBalancer"
	object.get(props(r), "Type", "application") == "application"
	not lb_logs_on(r)
}

lb_logs_on(r) if {
	some a in props(r).LoadBalancerAttributes
	a.Key == "access_logs.s3.enabled"
	lib.is_true(a.Value)
}

# TQR003: TLS 1.2 minimum.
deny contains lib.result("TQR003", id, sprintf("SSL policy %v allows TLS below 1.2", [p])) if {
	some id, r in input.Resources
	r.Type == "AWS::ElasticLoadBalancingV2::Listener"
	p := props(r).SslPolicy
	p in lib.legacy_ssl_policies
}

# TQR004: backup retention on production databases.
deny contains lib.result("TQR004", id, sprintf("backup retention is %v days, below %v on a production database", [d, lib.min_retention_days])) if {
	lib.prod
	some id, r in input.Resources
	r.Type in {"AWS::RDS::DBInstance", "AWS::RDS::DBCluster"}
	d := lib.num(props(r).BackupRetentionPeriod)
	d < lib.min_retention_days
}

# TQR005: no plain-HTTP listeners.
deny contains lib.result("TQR005", id, "an HTTP listener that does not redirect to HTTPS") if {
	some id, r in input.Resources
	r.Type == "AWS::ElasticLoadBalancingV2::Listener"
	props(r).Protocol == "HTTP"
	not redirects_https(r)
}

redirects_https(r) if {
	some a in props(r).DefaultActions
	a.Type == "redirect"
	a.RedirectConfig.Protocol == "HTTPS"
}

# TQR006: key rotation.
deny contains lib.result("TQR006", id, "automatic key rotation is off on a symmetric KMS key") if {
	some id, r in input.Resources
	r.Type == "AWS::KMS::Key"
	symmetric(r)
	not lib.is_true(props(r).EnableKeyRotation)
}

symmetric(r) if not lib.has_key(props(r), "KeySpec")

symmetric(r) if props(r).KeySpec == "SYMMETRIC_DEFAULT"
