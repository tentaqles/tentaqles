package policy

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/cloud/model"
	"github.com/tentaqles/tentaqles/internal/redact"
)

// Rule ids of the built-in IaC policy (spec D §7.2).
const (
	IAC001 = "IAC001" // stateful delete or replace
	IAC002 = "IAC002" // admin/database port open to the internet
	IAC003 = "IAC003" // public object storage
	IAC004 = "IAC004" // encryption at rest off
	IAC005 = "IAC005" // database open to the internet
	IAC006 = "IAC006" // deletion protection off in prod
	IAC007 = "IAC007" // ARM complete-mode deployment
	// IAC000 marks a source file the readers could not read (warn).
	IAC000 = "IAC000"
	// SUP001 is a tq-allow suppression without a reason (error).
	SUP001 = "SUP001"
)

// Rules lists the rule ids, for documentation and tests.
var Rules = []string{IAC001, IAC002, IAC003, IAC004, IAC005, IAC006, IAC007}

// AdminPorts are the admin and database ports IAC002 protects.
var AdminPorts = []int64{22, 3389, 1433, 3306, 5432, 6379, 27017}

// Outcome is what Evaluate found over a set of documents.
type Outcome struct {
	Findings   []packs.Finding
	Suppressed map[string]int // rule -> count of valid suppressions applied
	Resources  int
}

type hit struct {
	rule, level, msg string
}

// Evaluate runs IAC001…IAC007 over every resource of docs. A resource that
// carries a valid tq-allow suppression for a rule skips it; a suppression
// of an IAC rule without a reason is a SUP001 error.
func Evaluate(docs []Doc) Outcome {
	o := Outcome{Findings: []packs.Finding{}, Suppressed: map[string]int{}}
	for _, d := range docs {
		for _, r := range d.Resources {
			o.Resources++
			for _, s := range r.Suppressions() {
				if strings.HasPrefix(s.Rule, "IAC") && s.Reason == "" {
					o.Findings = append(o.Findings, finding(r, packs.LevelError, SUP001,
						fmt.Sprintf("%s: the tq-allow suppression of %s has no reason (write \"%s: <reason>\")", addr(r), s.Rule, s.Rule)))
				}
			}
			for _, h := range evalResource(d, r) {
				if s, ok := r.Allowed(h.rule); ok && s.Valid() {
					o.Suppressed[h.rule]++
					continue
				}
				o.Findings = append(o.Findings, finding(r, h.level, h.rule, addr(r)+": "+h.msg))
			}
		}
	}
	sort.SliceStable(o.Findings, func(i, j int) bool {
		a, b := o.Findings[i], o.Findings[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Rule < b.Rule
	})
	return o
}

func finding(r model.Resource, level, rule, msg string) packs.Finding {
	return packs.Finding{File: r.Pos.File, Line: r.Pos.Line, Level: level, Rule: rule, Message: clean(msg)}
}

// addr is the resource address for a message (redacted: for_each keys may
// hold anything).
func addr(r model.Resource) string {
	if r.Addr == "" {
		return r.Type
	}
	return r.Addr
}

const maxMessage = 300

func clean(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	s = redact.Redact(s, nil)
	if len(s) > maxMessage {
		s = s[:maxMessage] + "…"
	}
	return s
}

func evalResource(d Doc, r model.Resource) []hit {
	var hs []hit
	t := strings.ToLower(r.Type)
	if r.Planned() && model.Stateful(r.Type) && r.MayDestroy() {
		what := "is deleted"
		switch {
		case r.Has(model.ActReplace):
			what = "is replaced (deleted and created again)"
		case !r.Destroys():
			what = "may be deleted (the plan does not say)"
		}
		level := packs.LevelWarn
		if d.Approve() {
			level = packs.LevelError
		}
		hs = append(hs, hit{IAC001, level, fmt.Sprintf("stateful %s %s; its data is lost unless it is backed up", r.Type, what)})
	}
	if !r.Changes() || r.After == nil {
		return hs
	}
	p := props(r)
	hs = append(hs, openIngress(t, p)...)
	hs = append(hs, publicStorage(t, p)...)
	hs = append(hs, encryptionOff(t, p)...)
	hs = append(hs, dbOpen(d, r, t, p)...)
	hs = append(hs, deletionProtection(d, r, t, p)...)
	if t == "microsoft.resources/deployments" && strings.EqualFold(r.Meta["deployment_mode"], "complete") {
		hs = append(hs, hit{IAC007, packs.LevelWarn, "a Complete-mode deployment deletes every resource of the scope that the template does not list; the apply gate treats it as a destroy"})
	}
	return hs
}

// props is where a resource's settings are: the ARM "properties" object for
// ARM templates and what-if payloads, the attributes otherwise.
func props(r model.Resource) map[string]any {
	a := r.After
	if r.Source == model.KindARM || r.Source == model.KindWhatIf {
		if p := model.Map(a["properties"]); p != nil {
			return p
		}
		return map[string]any{}
	}
	return a
}

// norm folds a key for matching across snake_case (Terraform), camelCase
// (Pulumi, ARM) and PascalCase (CloudFormation).
func norm(k string) string {
	return strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(k, "_", ""), "-", ""))
}

// get returns the first present attribute among names (folded keys).
func get(m map[string]any, names ...string) any {
	if m == nil {
		return nil
	}
	for _, n := range names {
		want := norm(n)
		for k, v := range m {
			if norm(k) == want && v != nil {
				return v
			}
		}
	}
	return nil
}

func getMap(m map[string]any, names ...string) map[string]any {
	return model.Map(get(m, names...))
}

// isTrue / isFalse read a boolean set explicitly (string forms too).
func isTrue(v any) bool  { b, ok := model.Bool(v); return ok && b }
func isFalse(v any) bool { b, ok := model.Bool(v); return ok && !b }

func str(v any) string { s, _ := model.Str(v); return s }

// --- IAC002: ingress from anywhere to admin or database ports --------------

func anywhere(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "0.0.0.0/0", "::/0", "*", "internet", "any", "0.0.0.0":
		return true
	}
	return false
}

func anyAnywhere(vs ...any) bool {
	for _, v := range vs {
		for _, s := range model.Strings(v) {
			if anywhere(s) {
				return true
			}
		}
	}
	return false
}

// portsHit reports the admin ports inside [from, to]; a missing bound or
// "all" protocol means every port.
func portsHit(from, to int64) []int64 {
	var out []int64
	for _, p := range AdminPorts {
		if p >= from && p <= to {
			out = append(out, p)
		}
	}
	return out
}

func allProtocol(v any) bool {
	s := strings.ToLower(str(v))
	return s == "-1" || s == "all" || s == "*"
}

func awsRule(rule map[string]any) []int64 {
	if allProtocol(get(rule, "protocol", "ip_protocol", "IpProtocol")) {
		return AdminPorts
	}
	from, okf := model.Int(get(rule, "from_port", "FromPort"))
	to, okt := model.Int(get(rule, "to_port", "ToPort"))
	if !okf && !okt {
		return nil
	}
	if !okt {
		to = from
	}
	if !okf {
		from = to
	}
	if from == -1 && to == -1 {
		return AdminPorts
	}
	return portsHit(from, to)
}

// azurePorts reads destination_port_range(s) / destinationPortRange(s).
func azurePorts(rule map[string]any) []int64 {
	var ranges []string
	ranges = append(ranges, model.Strings(get(rule, "destination_port_range", "destinationPortRange"))...)
	ranges = append(ranges, model.Strings(get(rule, "destination_port_ranges", "destinationPortRanges"))...)
	set := map[int64]bool{}
	for _, r := range ranges {
		for _, part := range strings.Split(r, ",") {
			part = strings.TrimSpace(part)
			if part == "*" {
				for _, p := range AdminPorts {
					set[p] = true
				}
				continue
			}
			lo, hi, found := strings.Cut(part, "-")
			a, err1 := strconv.ParseInt(strings.TrimSpace(lo), 10, 64)
			b := a
			var err2 error
			if found {
				b, err2 = strconv.ParseInt(strings.TrimSpace(hi), 10, 64)
			}
			if err1 != nil || err2 != nil {
				continue
			}
			for _, p := range portsHit(a, b) {
				set[p] = true
			}
		}
	}
	var out []int64
	for _, p := range AdminPorts {
		if set[p] {
			out = append(out, p)
		}
	}
	return out
}

func azureRuleOpen(rule map[string]any) []int64 {
	if pp := getMap(rule, "properties"); pp != nil {
		rule = pp
	}
	dir := strings.ToLower(str(get(rule, "direction")))
	acc := strings.ToLower(str(get(rule, "access")))
	if (dir != "" && dir != "inbound") || (acc != "" && acc != "allow") {
		return nil
	}
	if !anyAnywhere(get(rule, "source_address_prefix", "sourceAddressPrefix"), get(rule, "source_address_prefixes", "sourceAddressPrefixes")) {
		return nil
	}
	return azurePorts(rule)
}

func gcpOpen(p map[string]any) []int64 {
	if d := strings.ToLower(str(get(p, "direction"))); d != "" && d != "ingress" {
		return nil
	}
	if !anyAnywhere(get(p, "source_ranges", "sourceRanges")) {
		return nil
	}
	set := map[int64]bool{}
	for _, a := range model.List(get(p, "allow", "allowed")) {
		am := model.Map(a)
		proto := strings.ToLower(str(get(am, "protocol", "IPProtocol")))
		ports := model.Strings(get(am, "ports"))
		if proto == "all" || len(ports) == 0 && (proto == "tcp" || proto == "udp" || proto == "all") {
			for _, x := range AdminPorts {
				set[x] = true
			}
			continue
		}
		for _, pr := range ports {
			for _, x := range azurePorts(map[string]any{"destinationPortRange": pr}) {
				set[x] = true
			}
		}
	}
	var out []int64
	for _, x := range AdminPorts {
		if set[x] {
			out = append(out, x)
		}
	}
	return out
}

func portList(ps []int64) string {
	s := make([]string, len(ps))
	for i, p := range ps {
		s[i] = strconv.FormatInt(p, 10)
	}
	return strings.Join(s, ", ")
}

func openIngress(t string, p map[string]any) []hit {
	var ports []int64
	addPorts := func(ps []int64) {
		for _, x := range ps {
			dup := false
			for _, y := range ports {
				dup = dup || x == y
			}
			if !dup {
				ports = append(ports, x)
			}
		}
	}
	awsOpen := func(rule map[string]any) {
		if anyAnywhere(get(rule, "cidr_blocks", "cidrBlocks", "CidrIp", "cidr_ipv4", "cidrIpv4"),
			get(rule, "ipv6_cidr_blocks", "ipv6CidrBlocks", "CidrIpv6", "cidr_ipv6", "cidrIpv6")) {
			addPorts(awsRule(rule))
		}
	}
	switch t {
	case "aws_security_group", "aws:ec2/securitygroup:securitygroup", "aws::ec2::securitygroup", "aws-native:ec2:securitygroup":
		for _, rule := range model.List(get(p, "ingress", "SecurityGroupIngress")) {
			awsOpen(model.Map(rule))
		}
	case "aws_security_group_rule", "aws:ec2/securitygrouprule:securitygrouprule":
		if strings.EqualFold(str(get(p, "type")), "ingress") {
			awsOpen(p)
		}
	case "aws_vpc_security_group_ingress_rule", "aws:vpc/securitygroupingressrule:securitygroupingressrule", "aws::ec2::securitygroupingress":
		awsOpen(p)
	case "azurerm_network_security_rule", "microsoft.network/networksecuritygroups/securityrules":
		addPorts(azureRuleOpen(p))
	case "azurerm_network_security_group", "microsoft.network/networksecuritygroups":
		for _, rule := range model.List(get(p, "security_rule", "securityRules")) {
			addPorts(azureRuleOpen(model.Map(rule)))
		}
	case "google_compute_firewall", "gcp:compute/firewall:firewall":
		addPorts(gcpOpen(p))
	}
	if len(ports) == 0 {
		return nil
	}
	sort.Slice(ports, func(i, j int) bool { return ports[i] < ports[j] })
	return []hit{{IAC002, packs.LevelError, "ingress from anywhere (0.0.0.0/0, ::/0 or *) reaches admin or database port " + portList(ports) + "; restrict the source to known ranges or use a bastion/private endpoint"}}
}

// --- IAC003: public object storage -------------------------------------------

func publicACL(v any) bool {
	switch norm(str(v)) {
	case "publicread", "publicreadwrite":
		return true
	}
	return false
}

func publicMember(v any) bool {
	for _, s := range model.Strings(v) {
		l := strings.ToLower(s)
		if l == "allusers" || l == "allauthenticatedusers" || strings.HasPrefix(l, "allusers:") || strings.HasPrefix(l, "allauthenticatedusers:") {
			return true
		}
	}
	return false
}

var publicAccessBlockKeys = []string{"block_public_acls", "block_public_policy", "ignore_public_acls", "restrict_public_buckets"}

func blockOff(m map[string]any) []string {
	var off []string
	for _, k := range publicAccessBlockKeys {
		if isFalse(get(m, k)) {
			off = append(off, k)
		}
	}
	return off
}

func publicStorage(t string, p map[string]any) []hit {
	h := func(msg string) []hit { return []hit{{IAC003, packs.LevelError, msg}} }
	switch t {
	case "aws_s3_bucket_acl", "aws_s3_bucket", "aws:s3/bucket:bucket", "aws:s3/bucketv2:bucketv2", "aws:s3/bucketaclv2:bucketaclv2", "aws::s3::bucket":
		if publicACL(get(p, "acl", "AccessControl")) {
			return h("the bucket ACL makes it public (public-read or public-read-write)")
		}
		if t == "aws::s3::bucket" {
			if off := blockOff(getMap(p, "PublicAccessBlockConfiguration")); len(off) > 0 {
				return h("the S3 public access block is disabled (" + strings.Join(off, ", ") + ")")
			}
		}
	case "aws_s3_bucket_public_access_block", "aws_s3_account_public_access_block",
		"aws:s3/bucketpublicaccessblock:bucketpublicaccessblock", "aws:s3/accountpublicaccessblock:accountpublicaccessblock":
		if off := blockOff(p); len(off) > 0 {
			return h("the S3 public access block is disabled (" + strings.Join(off, ", ") + ")")
		}
	case "azurerm_storage_account", "microsoft.storage/storageaccounts", "azure-native:storage:storageaccount":
		if isTrue(get(p, "allow_nested_items_to_be_public", "allow_blob_public_access", "allowBlobPublicAccess")) {
			return h("the storage account allows public blob access (allowBlobPublicAccess / allow_nested_items_to_be_public)")
		}
	case "azurerm_storage_container", "microsoft.storage/storageaccounts/blobservices/containers":
		switch strings.ToLower(str(get(p, "container_access_type", "publicAccess"))) {
		case "blob", "container":
			return h("the storage container allows anonymous read access")
		}
	case "google_storage_bucket_iam_member", "google_storage_bucket_iam_binding", "google_storage_bucket_acl", "google_storage_default_object_acl", "google_storage_object_acl":
		if publicMember(get(p, "member")) || publicMember(get(p, "members")) || publicMember(get(p, "role_entity")) {
			return h("the bucket is granted to allUsers or allAuthenticatedUsers")
		}
	}
	return nil
}

// --- IAC004: encryption at rest off ----------------------------------------

func encryptionOff(t string, p map[string]any) []hit {
	h := func(msg string) []hit { return []hit{{IAC004, packs.LevelError, msg}} }
	switch t {
	case "aws_db_instance", "aws_rds_cluster", "aws:rds/instance:instance", "aws:rds/cluster:cluster", "aws::rds::dbinstance", "aws::rds::dbcluster":
		if isFalse(get(p, "storage_encrypted", "StorageEncrypted")) {
			return h("storage encryption is off (storage_encrypted / StorageEncrypted = false)")
		}
	case "aws_ebs_volume", "aws:ebs/volume:volume", "aws::ec2::volume":
		if isFalse(get(p, "encrypted", "Encrypted")) {
			return h("the volume is not encrypted")
		}
	case "azurerm_mssql_database":
		if isFalse(get(p, "transparent_data_encryption_enabled")) {
			return h("transparent data encryption is off")
		}
	case "microsoft.sql/servers/databases/transparentdataencryption":
		if strings.EqualFold(str(get(p, "state", "status")), "disabled") {
			return h("transparent data encryption is off")
		}
	case "microsoft.storage/storageaccounts":
		for _, svc := range []string{"blob", "file", "table", "queue"} {
			if isFalse(model.Get(get(p, "encryption"), "services", svc, "enabled")) {
				return h("storage service encryption is off for " + svc)
			}
		}
	}
	return nil
}

// --- IAC005: database open to the internet ----------------------------------

func dbOpen(d Doc, r model.Resource, t string, p map[string]any) []hit {
	level := packs.LevelWarn
	if d.Prod() {
		level = packs.LevelError
	}
	switch t {
	case "azurerm_mssql_firewall_rule", "azurerm_sql_firewall_rule", "azurerm_postgresql_firewall_rule",
		"azurerm_postgresql_flexible_server_firewall_rule", "azurerm_mysql_flexible_server_firewall_rule",
		"microsoft.sql/servers/firewallrules", "microsoft.dbforpostgresql/flexibleservers/firewallrules",
		"microsoft.dbformysql/flexibleservers/firewallrules", "microsoft.dbforpostgresql/servers/firewallrules":
		start := str(get(p, "start_ip_address", "startIpAddress"))
		end := str(get(p, "end_ip_address", "endIpAddress"))
		if start == "0.0.0.0" && end == "255.255.255.255" {
			return []hit{{IAC005, level, "the database firewall allows every address (0.0.0.0–255.255.255.255)"}}
		}
	case "aws_db_instance", "aws:rds/instance:instance", "aws::rds::dbinstance", "aws_rds_cluster_instance":
		if isTrue(get(p, "publicly_accessible", "PubliclyAccessible")) {
			return []hit{{IAC005, level, "the database instance is publicly accessible"}}
		}
	case "azurerm_mssql_server", "azurerm_postgresql_flexible_server", "azurerm_mysql_flexible_server", "azurerm_cosmosdb_account",
		"microsoft.sql/servers", "microsoft.dbforpostgresql/flexibleservers", "microsoft.dbformysql/flexibleservers", "microsoft.documentdb/databaseaccounts":
		if !d.Prod() {
			return nil
		}
		v := get(p, "public_network_access_enabled", "publicNetworkAccess")
		if v == nil {
			v = get(getMap(p, "network"), "publicNetworkAccess")
		}
		if isTrue(v) || strings.EqualFold(str(v), "enabled") {
			return []hit{{IAC005, packs.LevelError, "public network access is enabled on a production database"}}
		}
	}
	return nil
}

// --- IAC006: deletion protection off in prod ---------------------------------

func deletionProtection(d Doc, r model.Resource, t string, p map[string]any) []hit {
	// Templates read from source have no environment: the rule warns there.
	level := ""
	switch {
	case d.Prod():
		level = packs.LevelError
	case d.Plan == nil && r.Env == "":
		level = packs.LevelWarn
	default:
		return nil
	}
	h := func(msg string) []hit { return []hit{{IAC006, level, msg}} }
	switch t {
	case "aws_db_instance", "aws_rds_cluster", "aws:rds/instance:instance", "aws:rds/cluster:cluster", "aws::rds::dbinstance", "aws::rds::dbcluster":
		if isFalse(get(p, "deletion_protection", "DeletionProtection")) {
			return h("deletion protection is off on a database")
		}
	case "azurerm_key_vault", "microsoft.keyvault/vaults":
		if isFalse(get(p, "purge_protection_enabled", "enablePurgeProtection")) {
			return h("purge protection is off on a key vault")
		}
	case "aws_dynamodb_table", "aws::dynamodb::table":
		if isFalse(get(p, "deletion_protection_enabled", "DeletionProtectionEnabled")) {
			return h("deletion protection is off on a DynamoDB table")
		}
	}
	if r.Source == model.KindCFN && model.Stateful(r.Type) {
		switch r.Meta["deletion_policy"] {
		case "Retain", "Snapshot":
		default:
			pol := r.Meta["deletion_policy"]
			if pol == "" {
				pol = "Delete (the default)"
			}
			return h("the stateful resource has DeletionPolicy " + pol + "; use Retain or Snapshot")
		}
	}
	return nil
}
