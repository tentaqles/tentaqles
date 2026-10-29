package guardcfg

// neverDisable are rule ids no layer can switch off: guard.disable and the
// global file's disable list skip them (spec D §16, decided at spec
// review). tq/hub-store-write protects the hub store, the pack ledgers and
// the saved plans under $TQ_HOME; with it off, an agent could forge an
// approval. Its rules are spec D's (internal/policy/builtin_packs.go).
var neverDisable = map[string]bool{
	"tq/hub-store-write": true,
}

// NeverDisable reports whether id ignores guard.disable.
func NeverDisable(id string) bool { return neverDisable[id] }

// dropNeverDisable removes the never-disable ids from the disable origins.
func dropNeverDisable(origin map[string]string) {
	for id := range origin {
		if neverDisable[id] {
			delete(origin, id)
		}
	}
}
