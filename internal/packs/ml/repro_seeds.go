package ml

// ml/seeds (spec C §4.3): a changed training file sets its seeds, and its
// randomised sklearn estimators and splitters get random_state.

import (
	"context"
	"fmt"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs"
)

const (
	ruleSeedMissing = "seed-missing"
	ruleSeedTorch   = "seed-torch"
	ruleSeedCUDA    = "seed-cuda"
	ruleSeedRNG     = "seed-unseeded-rng"
	ruleRandomState = "random-state"
	ruleDeterminism = "torch-deterministic"
	ruleLoaderSeed  = "dataloader-seed"
)

// randomStateCtors are estimators and splitters whose result depends on
// random_state by default. KFold and StratifiedKFold only with shuffle=True;
// train_test_split unless shuffle=False.
var randomStateCtors = set("RandomForestClassifier", "RandomForestRegressor", "ExtraTreesClassifier", "ExtraTreesRegressor",
	"GradientBoostingClassifier", "GradientBoostingRegressor", "HistGradientBoostingClassifier", "HistGradientBoostingRegressor",
	"DecisionTreeClassifier", "DecisionTreeRegressor", "AdaBoostClassifier", "AdaBoostRegressor", "BaggingClassifier",
	"BaggingRegressor", "IsolationForest", "SGDClassifier", "SGDRegressor", "MLPClassifier", "MLPRegressor", "KMeans",
	"MiniBatchKMeans", "TruncatedSVD", "TSNE", "ShuffleSplit", "StratifiedShuffleSplit", "GroupShuffleSplit", "RepeatedKFold",
	"RepeatedStratifiedKFold", "RandomizedSearchCV", "XGBClassifier", "XGBRegressor", "LGBMClassifier", "LGBMRegressor",
	"CatBoostClassifier", "CatBoostRegressor", "train_test_split", "KFold", "StratifiedKFold")

// seedHelperName is the function of `seed_helper: path.py:func`.
func seedHelperName(in *packs.Input) string {
	h := in.Settings().String("seed_helper")
	if i := strings.LastIndex(h, ":"); i >= 0 {
		return strings.TrimSpace(h[i+1:])
	}
	return ""
}

func runSeeds(ctx context.Context, in *packs.Input) packs.Result {
	files, fs := selectFiles(ctx, in, false)
	helper := seedHelperName(in)
	n := 0
	for _, s := range files {
		if !s.training {
			continue
		}
		n++
		fs = append(fs, seedFile(s.f, helper)...)
	}
	return packs.Result{Status: packs.OK, Detail: "seeds checked in " + plural(n, "training file"), Findings: fs}
}

func hasRealArg(c pcall) bool {
	for _, a := range c.Args {
		if a.Text != "None" && a.Text != "" {
			return true
		}
	}
	return false
}

func seedFile(f *pyFile, helper string) []packs.Finding {
	var out []packs.Finding
	add := func(i int, level, rule, msg string) {
		if !f.suppressed(i, rule, "ml/seeds") {
			out = append(out, f.find(i, level, rule, msg))
		}
	}
	torch := f.imports("torch")
	var anySeed, torchSeed, cudaSeed, helperCalled, deterministic, cuda bool
	firstTorch := 0
	for i, l := range f.Lines {
		if strings.Contains(l.Code, "cuda") || containsStr(l.Strings, "cuda") || strings.Contains(strings.Join(l.Strings, " "), "cuda:") {
			cuda = true
		}
		for _, c := range f.Calls[i] {
			r := c.Receiver
			switch {
			case helper != "" && c.Name == helper:
				helperCalled = true
			case c.Name == "seed_everything" || c.Name == "set_random_seed" || (c.Name == "set_seed" && (r != "" || f.fromImp["set_seed"] != "")):
				if hasRealArg(c) {
					anySeed, torchSeed, cudaSeed = true, true, true
				}
			case c.Name == "seed" && (r == "random" || strings.HasSuffix(r, "np.random") || strings.HasSuffix(r, "numpy.random")):
				if hasRealArg(c) {
					anySeed = true
				}
			case c.Name == "manual_seed_all" || (c.Name == "manual_seed" && strings.Contains(r, "cuda")):
				if hasRealArg(c) {
					anySeed, cudaSeed = true, true
				}
			case c.Name == "manual_seed":
				if hasRealArg(c) {
					anySeed = true
					if r == "torch" {
						torchSeed = true
					}
				}
			case c.Name == "default_rng" || c.Name == "RandomState" || (c.Name == "Random" && r == "random"):
				if hasRealArg(c) {
					anySeed = true
				} else {
					add(i, packs.LevelError, ruleSeedRNG, c.Name+"() without a seed draws from the OS entropy; pass an int")
				}
			case c.Name == "use_deterministic_algorithms":
				deterministic = true
			}
		}
	}
	for _, im := range f.Imports {
		if im.Module == "torch" || strings.HasPrefix(im.Module, "torch.") {
			firstTorch = indexOfLine(f, im.Line)
			break
		}
	}
	if helperCalled {
		anySeed, torchSeed, cudaSeed = true, true, true
	}
	helperHint := ""
	if helper != "" {
		helperHint = ", or call the seed_helper " + helper + "()"
	}
	switch {
	case !anySeed:
		add(-1, packs.LevelError, ruleSeedMissing, "this training file sets no seed (random.seed, np.random.seed or default_rng(<int>), torch.manual_seed"+helperHint+")")
	case torch && !torchSeed:
		add(firstTorch, packs.LevelError, ruleSeedTorch, "torch is used but torch.manual_seed is not set"+helperHint)
	case torch && cuda && !cudaSeed:
		add(firstTorch, packs.LevelError, ruleSeedCUDA, "CUDA is used but torch.cuda.manual_seed_all is not set"+helperHint)
	}
	if torch && !deterministic {
		add(firstTorch, packs.LevelWarn, ruleDeterminism, "torch.use_deterministic_algorithms(True) is not set")
	}
	for i := range f.Lines {
		for _, c := range f.Calls[i] {
			if torch && c.Name == "DataLoader" {
				_, w := c.kw("worker_init_fn")
				_, g := c.kw("generator")
				if !w && !g && !c.hasStar() {
					add(i, packs.LevelWarn, ruleLoaderSeed, "DataLoader is built without worker_init_fn or generator; worker shuffling is not seeded")
				}
			}
			if !randomStateCtors[c.Name] || c.hasStar() || strings.HasPrefix(c.Func, ".") && c.Name != "train_test_split" {
				continue
			}
			switch c.Name {
			case "KFold", "StratifiedKFold":
				if a, ok := c.kw("shuffle"); !ok || a.Text != "True" {
					continue
				}
			case "train_test_split":
				if a, ok := c.kw("shuffle"); ok && a.Text == "False" {
					continue
				}
			}
			a, ok := c.kw("random_state")
			if !ok {
				a, ok = c.kw("random_seed") // CatBoost
			}
			if !ok || a.Text == "None" {
				add(i, packs.LevelError, ruleRandomState, fmt.Sprintf("%s is built without random_state", c.Name))
			}
		}
	}
	return out
}

func indexOfLine(f *pyFile, line int) int {
	for i, l := range f.Lines {
		if l.Line == line {
			return i
		}
	}
	return 0
}
