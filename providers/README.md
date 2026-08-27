# Providers

The provider catalog lives at `internal/providers/catalog/*.yaml`, not here.
This folder is a pointer for humans browsing the repo root — see
[`docs/PROVIDERS.md`](../docs/PROVIDERS.md) for the schema and how to add a
provider, or the package doc in
[`internal/providers/provider.go`](../internal/providers/provider.go).

User overrides (and custom, non-catalog providers) live in
`~/.tentaqles/providers/*.yaml` and are merged on top of the embedded catalog
by id at load time — see `tq providers add`.
