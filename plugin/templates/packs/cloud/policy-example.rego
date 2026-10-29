# Example project policy for the tentaqles cloud pack (MIT).
#
# Copy it into a directory listed in packs.cloud.conftest.policy (for example
# policy/). tq runs Conftest with the shipped bundle plus these directories, one
# namespace per input kind: tq.terraform (plan JSON), tq.arm, tq.cfn. Rego v1.
#
# Every deny entry is an object with "msg", "addr" and "rule". Conftest puts the
# fields other than msg under "metadata", and tq reads metadata.addr to place the
# finding at the resource. Messages name the resource, never a planned value:
# planned values can be secrets.
#
# Prefix your rule ids and helper names with your project (here ACME / acme_) so
# they never collide with the shipped bundle in the same package.

package tq.terraform

import rego.v1

acme_allowed_db_classes := {"db.t4g.micro", "db.t4g.small", "db.r6g.large"}

acme_writes(rc) if {
	some action in rc.change.actions
	action in {"create", "update"}
}

# ACME001: database instances use an instance class from the approved list.
deny contains {"msg": msg, "addr": rc.address, "rule": "ACME001"} if {
	some rc in input.resource_changes
	rc.type == "aws_db_instance"
	acme_writes(rc)

	# object.get: `not` over a missing property is undefined, so a class not
	# known at plan time would pass; read it with a default instead.
	not object.get(object.get(rc.change, "after", {}), "instance_class", null) in acme_allowed_db_classes
	msg := sprintf("%s uses an instance class outside the approved list", [rc.address])
}
