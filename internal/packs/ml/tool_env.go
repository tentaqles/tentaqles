package ml

// The environment the ml pack's checks pass to their tools (Check.Env).
// toolrun passes only its base list (PATH, HOME, USERPROFILE, TEMP, TMP,
// TMPDIR, SystemRoot, ComSpec, PATHEXT, LANG, LC_*) plus these names. No
// credential variable is declared: a private index's credentials come from
// uv's or poetry's own config, keyring or netrc, not from the environment.

// userDirs are the user config and cache dirs (XDG on Unix,
// APPDATA/LOCALAPPDATA on Windows).
var userDirs = []string{"XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_DATA_HOME", "APPDATA", "LOCALAPPDATA"}

// proxyEnv reaches a package index through a proxy and a custom CA bundle.
var proxyEnv = []string{"HTTPS_PROXY", "HTTP_PROXY", "NO_PROXY", "https_proxy", "http_proxy", "no_proxy",
	"SSL_CERT_FILE", "SSL_CERT_DIR", "REQUESTS_CA_BUNDLE"}

// ruffEnv: ruff reads the repo's config, then the user config dir.
var ruffEnv = append([]string{"RUFF_CACHE_DIR"}, userDirs...)

// uvEnv: uv lock --check may resolve against the index (network). The
// index URLs are passed by name (a URL that embeds a credential is the
// user's choice); uv's per-index USERNAME/PASSWORD variables are not.
var uvEnv = append(append([]string{"UV_CACHE_DIR", "UV_CONFIG_FILE", "UV_NO_CONFIG", "UV_INDEX", "UV_DEFAULT_INDEX",
	"UV_INDEX_URL", "UV_EXTRA_INDEX_URL", "UV_FIND_LINKS", "UV_PYTHON", "UV_PYTHON_INSTALL_DIR", "UV_OFFLINE",
	"UV_NATIVE_TLS"}, userDirs...), proxyEnv...)

// poetryEnv: poetry check --lock reads its config and cache dirs only.
var poetryEnv = append([]string{"POETRY_HOME", "POETRY_CONFIG_DIR", "POETRY_CACHE_DIR", "POETRY_DATA_DIR"}, userDirs...)

// dvcEnv: dvc status (no remote) reads the global and site config and runs
// git.
var dvcEnv = append([]string{"DVC_GLOBAL_CONFIG_DIR", "DVC_SITE_CACHE_DIR", "GIT_DIR", "GIT_WORK_TREE"}, userDirs...)
