package tools_test

import "github.com/tentaqles/tentaqles/internal/packs/toolrun"

// The fake IaC tools of this package's tests read these variables;
// toolrun's filtered environment passes them to every run in the test
// binary (and nothing else of the test's environment).
func init() {
	names := []string{"TQ_FAKE_CLOUD", "TQ_FAKE_ARGV_DIR", "TQ_FAKE_ENV_NAMES"}
	for _, tool := range []string{"CHECKOV", "TRIVY", "CONFTEST", "TFLINT", "BICEP", "CFN_LINT"} {
		for _, p := range []string{"TQ_FAKE_VERSION_", "TQ_FAKE_OUT_", "TQ_FAKE_EXIT_", "TQ_FAKE_HANG_"} {
			names = append(names, p+tool)
		}
	}
	toolrun.PassForTests(names...)
}
