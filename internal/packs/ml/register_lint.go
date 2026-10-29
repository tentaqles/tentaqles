package ml

// Registration of the M1 workstream (spec C §4.2, §4.3): the leakage lint,
// the reproducibility checks, their external companions, the config keys
// they read (paths, seed_helper, data_manifest) and the ml triage flag.

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs"
	"gopkg.in/yaml.v3"
)

func init() {
	packs.RegisterPack(packs.Pack{
		Name: "ml",
		ConfigKeys: map[string]func(yaml.Node) error{
			"paths":         validatePaths,
			"seed_helper":   validateSeedHelper,
			"data_manifest": validateRepoPath,
		},
		Merge: map[string]packs.MergeRule{
			"paths.train":    packs.Union(),
			"paths.features": packs.Union(),
			"paths.eval":     packs.Union(),
			// Added when the manifest lacks them, never changed.
			"seed_helper":   packs.TrustedAdd(),
			"data_manifest": packs.TrustedAdd(),
		},
		Flags: []packs.Flag{{
			Name:      "ml",
			Reviewers: []string{"ml-reviewer"},
			Raise:     raiseML,
			Question:  "Does this change alter how a model is trained or evaluated, or which data reaches its features?",
		}},
		Hints: []packs.Hint{
			{Glob: "*.py", Contains: `(?m)^\s*(from|import)\s+(sklearn|torch|xgboost|lightgbm|keras|tensorflow)\b`, Why: "ML training code (scikit-learn, PyTorch, XGBoost, LightGBM, Keras)"},
			{Glob: "dvc.yaml", Why: "a DVC pipeline"},
		},
	})

	pyChanged := func(in *packs.Input) bool {
		ps := pathsOf(in)
		for _, p := range in.ChangedPaths() {
			if ps.candidate(p) {
				return true
			}
		}
		return false
	}
	builtin := func(id string, applies func(*packs.Input) bool, run func(context.Context, *packs.Input) packs.Result) {
		packs.Register(packs.Check{ID: id, Pack: "ml", Kind: packs.Builtin, Applies: applies, Run: run})
	}
	builtin("ml/leakage", pyChanged, runLeakage)
	builtin("ml/seeds", pyChanged, runSeeds)
	builtin("ml/data-snapshot", pyChanged, runSnapshot)
	builtin("ml/run-tracking", pyChanged, runTracking)
	builtin("ml/env-lock", func(in *packs.Input) bool { return in.AnyChanged(envLockGlobs...) }, runEnvLock)
	builtin("ml/unsafe-deserialize", func(in *packs.Input) bool { return in.AnyChanged("*.py", "*.ipynb") }, runDeserialize)
	builtin("ml/notebook-outputs", func(in *packs.Input) bool { return in.AnyChanged("*.ipynb") }, runNotebookOutputs)

	packs.Register(packs.Check{ID: "ml/ruff", Pack: "ml", Kind: packs.External, Tool: "ruff", MinVersion: "0.6.0", Timeout: toolTimeout, Env: ruffEnv,
		Applies: func(in *packs.Input) bool { return in.AnyChanged("*.py", "*.ipynb") }, Run: runRuff})
	packs.Register(packs.Check{ID: "ml/lock-check", Pack: "ml", Kind: packs.External, Tool: "uv", Timeout: toolTimeout, Env: uvEnv,
		Applies: func(in *packs.Input) bool { return in.AnyChanged("pyproject.toml", "uv.lock") },
		Run:     lockCheck("uv", "uv.lock", []string{"lock", "--check"})})
	packs.Register(packs.Check{ID: "ml/lock-check-poetry", Pack: "ml", Kind: packs.External, Tool: "poetry", Timeout: toolTimeout, Env: poetryEnv,
		Applies: func(in *packs.Input) bool { return in.AnyChanged("pyproject.toml", "poetry.lock") },
		Run:     lockCheck("poetry", "poetry.lock", []string{"check", "--lock"})})
	packs.Register(packs.Check{ID: "ml/dvc-status", Pack: "ml", Kind: packs.External, Tool: "dvc", Timeout: toolTimeout, Env: dvcEnv,
		Applies: dvcApplies, Run: runDVCStatus})
}

// raiseML raises the ml flag (spec C §5.4): a changed file under ml.paths,
// or, when paths.train is empty, a changed training file. The content probe
// is bounded (flagProbeFiles files, flagProbeBytes each).
func raiseML(in *packs.Input) bool {
	ps := pathsOf(in)
	if in.AnyChanged(ps.any()...) {
		return true
	}
	if len(ps.train) > 0 {
		return false
	}
	n := 0
	for _, p := range in.ChangedPaths() {
		if !pyLike(p) {
			continue
		}
		if n++; n > flagProbeFiles {
			return false
		}
		b, err := in.ReadNew(p, flagProbeBytes)
		if err != nil {
			continue
		}
		if trainRe.Match(b) {
			return true
		}
	}
	return false
}

var trainRe = regexp.MustCompile(`\.fit\s*\(|optim\w*\.step\s*\(|trainer\.train\s*\(`)

// --- config validation ----------------------------------------------------------

func validatePaths(n yaml.Node) error {
	if n.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: must be a mapping of train, features and eval path lists", n.Line)
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		switch k.Value {
		case "train", "features", "eval":
		default:
			return fmt.Errorf("line %d: unknown key %q (known: train, features, eval)", k.Line, k.Value)
		}
		if v.Kind != yaml.SequenceNode {
			return fmt.Errorf("line %d: %s must be a list of globs", v.Line, k.Value)
		}
		for _, g := range v.Content {
			if g.Kind != yaml.ScalarNode || strings.TrimSpace(g.Value) == "" {
				return fmt.Errorf("line %d: %s holds an empty or non-string glob", g.Line, k.Value)
			}
			if err := checkRel(g.Value); err != nil {
				return fmt.Errorf("line %d: %s: %v", g.Line, k.Value, err)
			}
		}
	}
	return nil
}

var seedHelperRe = regexp.MustCompile(`^[^:]+\.py:[A-Za-z_]\w*$`)

func validateSeedHelper(n yaml.Node) error {
	if n.Kind != yaml.ScalarNode || !seedHelperRe.MatchString(n.Value) {
		return fmt.Errorf("line %d: must be <path>.py:<function>, for example src/common/seed.py:set_seed", n.Line)
	}
	if err := checkRel(n.Value[:strings.LastIndex(n.Value, ":")]); err != nil {
		return fmt.Errorf("line %d: %v", n.Line, err)
	}
	return nil
}

func validateRepoPath(n yaml.Node) error {
	if n.Kind != yaml.ScalarNode || strings.TrimSpace(n.Value) == "" {
		return fmt.Errorf("line %d: must be a repo path", n.Line)
	}
	if err := checkRel(n.Value); err != nil {
		return fmt.Errorf("line %d: %v", n.Line, err)
	}
	return nil
}

// checkRel refuses absolute paths and .. segments.
func checkRel(p string) error {
	q := strings.ReplaceAll(p, `\`, "/")
	if strings.HasPrefix(q, "/") || (len(q) > 1 && q[1] == ':') || strings.HasPrefix(q, "~") {
		return errors.New("must be relative to the repo root")
	}
	for _, s := range strings.Split(q, "/") {
		if s == ".." {
			return errors.New("must not contain ..")
		}
	}
	return nil
}
