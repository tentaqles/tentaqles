# opa test for policy-example.rego (MIT). Run: opa test policy/
package tq.terraform_test

import rego.v1

import data.tq.terraform

acme_plan(class) := {"resource_changes": [{
	"address": "aws_db_instance.main",
	"type": "aws_db_instance",
	"change": {"actions": ["create"], "after": {"instance_class": class}},
}]}

acme001(denials) := {d | some d in denials; d.rule == "ACME001"}

test_denies_a_class_off_the_list if {
	denials := terraform.deny with input as acme_plan("db.x2iedn.32xlarge")
	some d in acme001(denials)
	d.addr == "aws_db_instance.main"
}

test_allows_an_approved_class if {
	denials := terraform.deny with input as acme_plan("db.t4g.small")
	count(acme001(denials)) == 0
}
