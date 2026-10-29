package policy

import (
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/packs/cloud/model"
)

// TestCFNClusterEncryptionInherited: an AWS::RDS::DBCluster without
// StorageEncrypted is encrypted when EngineMode is serverless (the CFN
// default for that mode is true) and when it is a secondary cluster
// (GlobalClusterIdentifier, ReplicationSourceIdentifier), which inherits
// encryption from its source. None of those raise IAC004 (review r1 B).
func TestCFNClusterEncryptionInherited(t *testing.T) {
	cases := []struct {
		name  string
		props map[string]any
		want  string
	}{
		{"serverless", map[string]any{"EngineMode": "serverless", "DeletionProtection": true}, ""},
		{"serverless any case", map[string]any{"EngineMode": "Serverless", "DeletionProtection": true}, ""},
		{"global secondary", map[string]any{"GlobalClusterIdentifier": "g", "DeletionProtection": true}, ""},
		{"replica", map[string]any{"ReplicationSourceIdentifier": "arn:aws:rds:x", "DeletionProtection": true}, ""},
		{"provisioned", map[string]any{"EngineMode": "provisioned", "DeletionProtection": true}, "IAC004:error"},
		{"serverless explicit false", map[string]any{"EngineMode": "serverless", "StorageEncrypted": false, "DeletionProtection": true}, "IAC004:error"},
		{"serverless intrinsic mode", map[string]any{"EngineMode": map[string]any{"Ref": "Mode"}, "DeletionProtection": true}, "IAC004:error"},
	}
	for _, c := range cases {
		r := model.Resource{Addr: "C", Type: "AWS::RDS::DBCluster", Source: model.KindCFN, After: c.props}
		var got []string
		for _, f := range Evaluate([]Doc{{Name: "x", Resources: []model.Resource{r}}}).Findings {
			got = append(got, f.Rule+":"+f.Level)
		}
		if g := strings.Join(got, ","); g != c.want {
			t.Errorf("%s: got %q, want %q", c.name, g, c.want)
		}
	}
	// An instance (not a cluster) with EngineMode is not special-cased.
	r := model.Resource{Addr: "I", Type: "AWS::RDS::DBInstance", Source: model.KindCFN,
		After: map[string]any{"EngineMode": "serverless", "DeletionProtection": true}}
	fs := Evaluate([]Doc{{Name: "x", Resources: []model.Resource{r}}}).Findings
	if len(fs) != 1 || fs[0].Rule != IAC004 {
		t.Errorf("instance: %v", fs)
	}
}
