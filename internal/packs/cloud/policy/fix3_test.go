package policy

import (
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/cloud/model"
)

// TestGlobalClusterEncryption: the primary DBCluster of a global database
// sets GlobalClusterIdentifier and takes its encryption from the
// GlobalCluster, whose StorageEncrypted defaults to false. So the
// GlobalCluster itself is judged: absent or false is IAC004 unless it is
// created from an existing cluster (SourceDBClusterIdentifier), whose
// encryption it inherits (review r2 D).
func TestGlobalClusterEncryption(t *testing.T) {
	cases := []struct {
		name  string
		typ   string
		src   model.Kind
		props map[string]any
		want  string
	}{
		{"cfn absent", "AWS::RDS::GlobalCluster", model.KindCFN, map[string]any{"Engine": "aurora-postgresql"}, "IAC004:error"},
		{"cfn false", "AWS::RDS::GlobalCluster", model.KindCFN, map[string]any{"StorageEncrypted": false}, "IAC004:error"},
		{"cfn true", "AWS::RDS::GlobalCluster", model.KindCFN, map[string]any{"StorageEncrypted": true}, ""},
		{"cfn from source", "AWS::RDS::GlobalCluster", model.KindCFN, map[string]any{"SourceDBClusterIdentifier": "arn:aws:rds:x"}, ""},
		{"cfn intrinsic", "AWS::RDS::GlobalCluster", model.KindCFN, map[string]any{"StorageEncrypted": map[string]any{"Ref": "Enc"}}, ""},
		{"tf false", "aws_rds_global_cluster", model.KindTFPlan, map[string]any{"storage_encrypted": false}, "IAC004:error"},
		{"tf true", "aws_rds_global_cluster", model.KindTFPlan, map[string]any{"storage_encrypted": true}, ""},
		{"pulumi false", "aws:rds/globalCluster:GlobalCluster", model.KindPulumi, map[string]any{"storageEncrypted": false}, "IAC004:error"},
	}
	for _, c := range cases {
		r := model.Resource{Addr: "G", Type: c.typ, Source: c.src, After: c.props}
		var got []string
		for _, f := range Evaluate([]Doc{{Name: "x", Resources: []model.Resource{r}}}).Findings {
			if f.Rule == IAC004 {
				got = append(got, f.Rule+":"+f.Level)
			}
		}
		if g := strings.Join(got, ","); g != c.want {
			t.Errorf("%s: got %q, want %q", c.name, g, c.want)
		}
	}
	// The global database's primary cluster with nothing set on either
	// resource is caught once, on the GlobalCluster.
	tmpl := []model.Resource{
		{Addr: "G", Type: "AWS::RDS::GlobalCluster", Source: model.KindCFN, After: map[string]any{"Engine": "aurora-mysql"}},
		{Addr: "C", Type: "AWS::RDS::DBCluster", Source: model.KindCFN,
			After: map[string]any{"GlobalClusterIdentifier": map[string]any{"Ref": "G"}, "DeletionProtection": true}},
	}
	var fs []packs.Finding
	for _, f := range Evaluate([]Doc{{Name: "x", Resources: tmpl}}).Findings {
		if f.Rule == IAC004 {
			fs = append(fs, f)
		}
	}
	if len(fs) != 1 || !strings.HasPrefix(fs[0].Message, "G:") {
		t.Errorf("global template: %+v", fs)
	}
}
