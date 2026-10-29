package data

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
)

// The environment the data pack's tools need. toolrun passes a filtered
// environment (toolrun.BaseEnv: PATH, HOME, USERPROFILE, TEMP, TMP, TMPDIR,
// SystemRoot, ComSpec, PATHEXT, LANG and LC_*); each check names the other
// parent variables its tool reads in Check.Env (or Spec.PassEnv), and a
// declared name passes even when it looks like a secret. The lists are kept
// tight: no password or token variable is declared here, because each of
// these tools also reads its credentials from its own config file (a dbt
// profile, ~/.pgpass, ~/.databrickscfg, the Snowflake connections file, the
// gcloud or az login). The one exception is a dbt profile's env_var()
// names, read from profiles.yml: the profile itself says dbt reads them from
// the environment.

// pyUserDirs are where Python tools (platformdirs) keep user config and
// caches: XDG on Unix, APPDATA/LOCALAPPDATA on Windows.
var pyUserDirs = []string{"XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_DATA_HOME", "APPDATA", "LOCALAPPDATA"}

// proxyEnv reaches the network through a proxy and a custom CA bundle.
var proxyEnv = []string{"HTTPS_PROXY", "HTTP_PROXY", "NO_PROXY", "https_proxy", "http_proxy", "no_proxy",
	"SSL_CERT_FILE", "SSL_CERT_DIR", "REQUESTS_CA_BUNDLE", "CURL_CA_BUNDLE"}

// gcloudEnv: bq, and dbt-bigquery's oauth method, read the gcloud config dir
// (APPDATA\gcloud on Windows) and application default credentials (a path).
var gcloudEnv = []string{"CLOUDSDK_CONFIG", "CLOUDSDK_CORE_PROJECT", "GOOGLE_APPLICATION_CREDENTIALS", "GOOGLE_CLOUD_PROJECT", "APPDATA"}

// dbtEnv are dbt's own variables (the profile's env_var names are added at
// run time by dbtRunEnv).
var dbtEnv = concat([]string{"DBT_PROFILES_DIR", "DBT_PROJECT_DIR", "DBT_TARGET", "DBT_TARGET_PATH", "DBT_LOG_PATH",
	"DBT_PACKAGES_INSTALL_PATH", "DBT_PROFILE"}, pyUserDirs, proxyEnv, gcloudEnv)

// sqlfluffEnv: user config (~/.sqlfluff, the platformdirs config dir). With
// the dbt templater sqlfluff loads dbt, so it gets dbt's variables too.
var sqlfluffEnv = concat(pyUserDirs, dbtEnv)

// contractEnv: the datacontract CLI fetches the ODCS JSON schema, so it may
// need a proxy.
var contractEnv = concat(pyUserDirs, proxyEnv)

// dagsterEnv: definitions validate imports the user's code; DAGSTER_HOME is
// the instance. Variables the user code itself reads are not known here:
// set them in the definitions' own config, or the check reports the import
// error.
var dagsterEnv = concat([]string{"DAGSTER_HOME", "PYTHONPATH"}, pyUserDirs)

// estimateEnv are the plan-only CLIs' variables per engine (names only; the
// credentials come from the CLIs' own login and config files).
var estimateEnv = map[string][]string{
	engBigQuery: concat(gcloudEnv, proxyEnv),
	engSnowflake: concat([]string{"SNOWFLAKE_HOME", "SNOWFLAKE_CONNECTIONS_FILE", "SNOWFLAKE_DEFAULT_CONNECTION_NAME",
		"SNOWFLAKE_ACCOUNT", "SNOWFLAKE_USER", "SNOWFLAKE_ROLE", "SNOWFLAKE_WAREHOUSE", "SNOWFLAKE_AUTHENTICATOR",
		"LOCALAPPDATA", "APPDATA"}, proxyEnv),
	engDatabricks: concat([]string{"DATABRICKS_CONFIG_FILE", "DATABRICKS_CONFIG_PROFILE", "DATABRICKS_HOST", "DATABRICKS_AUTH_TYPE"}, proxyEnv),
	engPostgres:   {"PGHOST", "PGPORT", "PGUSER", "PGDATABASE", "PGSERVICE", "PGSERVICEFILE", "PGPASSFILE", "PGSSLMODE", "PGSSLROOTCERT", "PGAPPNAME", "PGCONNECT_TIMEOUT"},
	engSynapse:    concat([]string{"SQLCMDSERVER", "SQLCMDDBNAME", "SQLCMDUSER", "AZURE_CONFIG_DIR", "AZURE_TENANT_ID", "AZURE_CLIENT_ID", "APPDATA", "LOCALAPPDATA"}, proxyEnv),
	engFabric:     concat([]string{"SQLCMDSERVER", "SQLCMDDBNAME", "SQLCMDUSER", "AZURE_CONFIG_DIR", "AZURE_TENANT_ID", "AZURE_CLIENT_ID", "APPDATA", "LOCALAPPDATA"}, proxyEnv),
}

func concat(lists ...[]string) []string {
	seen := map[string]bool{}
	var out []string
	for _, l := range lists {
		for _, n := range l {
			if !seen[n] {
				seen[n] = true
				out = append(out, n)
			}
		}
	}
	return out
}

// envVarRe finds dbt's env_var('NAME') and env_var("NAME", default).
var envVarRe = regexp.MustCompile(`env_var\(\s*['"]([A-Za-z_][A-Za-z0-9_]{0,127})['"]`)

// maxProfileBytes bounds a profiles.yml or dbt_project.yml read.
const maxProfileBytes = 1 << 20

// dbtRunEnv are the env_var() names of the profiles.yml dbt would read for a
// project in dir (dir, then DBT_PROFILES_DIR, then ~/.dbt), and of the
// project's dbt_project.yml. Only names are read, never values.
func dbtRunEnv(dir string) []string {
	files := []string{filepath.Join(dir, "dbt_project.yml"), filepath.Join(dir, "profiles.yml")}
	if d := os.Getenv("DBT_PROFILES_DIR"); d != "" {
		files = append(files, filepath.Join(d, "profiles.yml"))
	}
	if h, err := os.UserHomeDir(); err == nil {
		files = append(files, filepath.Join(h, ".dbt", "profiles.yml"))
	}
	seen := map[string]bool{}
	for _, f := range files {
		fi, err := os.Stat(f)
		if err != nil || !fi.Mode().IsRegular() || fi.Size() > maxProfileBytes {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for _, m := range envVarRe.FindAllSubmatch(b, -1) {
			seen[string(m[1])] = true
		}
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
