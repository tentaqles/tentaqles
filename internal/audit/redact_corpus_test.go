package audit

import (
	"strings"
	"testing"
)

// The leak corpus of ADR-0006 round 2: every command carries a secret the
// log must never hold. The first block is the round-1 critic's set (it
// leaked 16 of 40 through the pattern redaction); the second block is a
// fresh set written for the structural rule. The structural rule keeps only
// command words, flag names, operators, variable references and a short
// list of subcommand words in the subcommand position; every other word is
// masked, so a secret in any argument shape is gone.
//
// Fixture values are fake (tq:allow-secret).
var leakCorpus = []struct {
	name, cmd string
	secrets   []string
}{
	// The critic's set (round 1).
	{"bearer header", `curl -H "Authorization: Bearer sk-live-Zq81Hh2" https://api.example.com`, []string{"sk-live-Zq81Hh2"}},
	{"bearer header unquoted-ish", "curl --header Authorization:Bearer_Qx9pLm2 https://x", []string{"Qx9pLm2"}},
	{"aws env prefix", "AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY aws s3 ls", []string{"AKIAIOSFODNN7EXAMPLE", "wJalrXUtnFEMI"}}, // tq:allow-secret
	{"aws configure set", "aws configure set aws_secret_access_key wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", []string{"wJalrXUtnFEMI"}},                                                     // tq:allow-secret
	{"aws key positional", "echo AKIAIOSFODNN7EXAMPLE | aws sts get-caller-identity", []string{"AKIAIOSFODNN7EXAMPLE"}},                                                                      // tq:allow-secret
	{"mysql -p glued", "mysql -uroot -pS3cretPw1 -h db.local", []string{"S3cretPw1"}},
	{"mysql --password=", "mysql -u root --password=Hunter2xx db", []string{"Hunter2xx"}},
	{"sqlcmd -P", `sqlcmd -S db.local -U sa -P Pa55w0rdQ -Q "select 1"`, []string{"Pa55w0rdQ"}},
	{"psql url", "psql postgres://app:PgPassZ9@db.local:5432/app", []string{"PgPassZ9"}},                                                       // tq:allow-secret
	{"mongodb url", "mongosh mongodb+srv://u:MongoPw77@cluster0.x.mongodb.net/db", []string{"MongoPw77"}},                                      // tq:allow-secret
	{"git clone with creds", "git clone https://renato:ghp_R4nd0mT0k3nV4lu3abcdefghijklmnop12@github.com/acme/x.git", []string{"R4nd0mT0k3n"}}, // tq:allow-secret
	{"git push https user:pass", "git push https://bob:BobPass123@github.com/acme/x.git main", []string{"BobPass123"}},
	{"curl -u", "curl -u admin:AdminPw42 https://jenkins.local/api", []string{"AdminPw42"}},
	{"curl --user", "curl --user admin:AdminPw43 https://jenkins.local/api", []string{"AdminPw43"}},
	{"curl --data json", `curl -X POST --data '{"password":"JsonPw88"}' https://x/login`, []string{"JsonPw88"}},
	{"curl -d unquoted", "curl -d password=FormPw99 https://x/login", []string{"FormPw99"}},
	{"powershell securestring", "$p = ConvertTo-SecureString 'PsPw1234' -AsPlainText -Force; New-PSDrive -Credential $p", []string{"PsPw1234"}},
	{"powershell -String param", "ConvertTo-SecureString -String PsBare55 -AsPlainText -Force", []string{"PsBare55"}},
	{"base64 blob", "echo dXNlcjpwYXNzd29yZDEyMzQ1Njc4OTA= | base64 -d", []string{"dXNlcjpwYXNzd29yZDEyMzQ1Njc4OTA="}},
	{"short base64 blob", "echo c2VjcmV0OnB3 | base64 -d", []string{"c2VjcmV0OnB3"}},
	{"jwt bare", "curl https://api.x/me?token=eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U", []string{"dozjgNryP4J3jVmNHl0w5N"}}, // tq:allow-secret
	{"jwt cookie", "curl -b session=eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U https://x", []string{"dozjgNryP4J3jVmNHl0w5N"}}, // tq:allow-secret
	{"export in pipeline", "export TOKEN=tok_Abc12345 && curl -H x https://x | jq .", []string{"tok_Abc12345"}},                                                                  // tq:allow-secret
	{"gh auth here-string", "gh auth login --with-token <<< ghp_R4nd0mT0k3nV4lu3abcdefghijklmnop12", []string{"R4nd0mT0k3n"}},                                                    // tq:allow-secret
	{"gh auth here-string short", "gh auth login --with-token <<< shortTok99", []string{"shortTok99"}},
	{"echo pipe to gh", "echo shortTok98 | gh auth login --with-token", []string{"shortTok98"}},
	{"sshpass", "sshpass -p hunter2pw ssh u@h", []string{"hunter2pw"}},
	{"docker login -p", "docker login -u bob -p DockPw12 registry.x", []string{"DockPw12"}},
	{"az login -p", "az login -u bob@x.com -p AzPw!2026", []string{"AzPw!2026"}},
	{"heredoc", "cat <<EOF > creds\npassword=HereDocPw1\nEOF", []string{"HereDocPw1"}},
	{"sqlplus slash", "sqlplus scott/TigerPw7@orcl", []string{"TigerPw7"}},
	{"redis-cli -a", "redis-cli -a RedisPw55 ping", []string{"RedisPw55"}},
	{"htpasswd -b", "htpasswd -b .htpasswd bob HtPw4455", []string{"HtPw4455"}},
	{"openssl -pass pass:", "openssl enc -aes256 -pass pass:OsslPw77 -in a", []string{"OsslPw77"}},
	{"npm token env inline", "NPM_TOKEN=npm_Xy12Ab34Cd56 npm publish", []string{"npm_Xy12Ab34Cd56"}}, // tq:allow-secret
	{"unicode quotes", "curl -H “Authorization: Bearer UniQt77” https://x", []string{"UniQt77"}},
	{"ps escape quotes", `Invoke-RestMethod -Headers @{Authorization="Bearer PsHdr66"} https://x`, []string{"PsHdr66"}},
	{"ps hashtable unquoted", "Invoke-RestMethod -Headers @{apikey=PsKey33} https://x", []string{"PsKey33"}},
	{"url query api_key", "curl https://x/api?api_key=QryKey44&q=1", []string{"QryKey44"}}, // tq:allow-secret
	{"git -c http.extraheader", `git -c http.extraheader="AUTHORIZATION: basic Zm9vOmJhcg==" push`, []string{"Zm9vOmJhcg=="}},

	// A fresh set (round 2).
	{"lowercase-only password after -p", "mysql -u app -p hunterpass", []string{"hunterpass"}},
	{"glued lowercase password", "mysql -phunterpass db", []string{"hunterpass"}},
	{"password as subcommand-looking word", "redis-cli -a deploy ping", []string{"-a deploy"}},
	{"secret as first echo argument", "echo status | gh auth login --with-token", []string{"echo status"}},
	{"printf secret", "printf '%s' s3cr3t | docker login --password-stdin", []string{"s3cr3t"}},
	{"secret glued to redirect", "echo topsecret>file.txt", []string{"topsecret"}},
	{"secret after semicolon as verb", "true; Sup3rS3cretT0ken", []string{"Sup3rS3cretT0ken"}},
	{"backtick substitution", "curl -H `cat tok` -d sec=Bq77xLm https://x", []string{"Bq77xLm"}},
	{"dollar paren substitution", "TOKEN=$(echo PlainPw8) gh api /user", []string{"PlainPw8"}},
	{"windows cmd set", "cmd /c set API_KEY=WinKey12 && run.exe", []string{"WinKey12"}},
	{"powershell env glued", "$env:GH_TOKEN='PsEnv77'; gh repo list", []string{"PsEnv77"}},
	{"powershell env unquoted", "$env:GH_TOKEN = PsEnvBare8; gh repo list", []string{"PsEnvBare8"}},
	{"powershell credential object", "New-Object PSCredential('bob', (ConvertTo-SecureString 'CredPw9' -AsPlainText -Force))", []string{"CredPw9"}},
	{"az sp login secret", "az login --service-principal -u 1111 -p ClientSecret~X9 --tenant t", []string{"ClientSecret~X9"}},
	{"gcloud key in flag value", "gcloud auth activate-service-account --key-file=/tmp/k.json --password=GcPw77", []string{"GcPw77"}},
	{"kubectl token flag", "kubectl --token=KubeTok123 get pods", []string{"KubeTok123"}},
	{"kubectl token spaced", "kubectl --token KubeTok124 get pods", []string{"KubeTok124"}},
	{"helm repo password", "helm repo add r https://charts --username u --password HelmPw5", []string{"HelmPw5"}},
	{"git credential approve", "printf 'protocol=https\\nhost=github.com\\npassword=GitCredPw1\\n' | git credential approve", []string{"GitCredPw1"}},
	{"ssh-keygen passphrase", "ssh-keygen -t ed25519 -N KeyPhrase7 -f k", []string{"KeyPhrase7"}},
	{"7z password glued", "7z x a.7z -pZipPw42", []string{"ZipPw42"}},
	{"unzip -P", "unzip -P UnzipPw3 a.zip", []string{"UnzipPw3"}},
	{"psql PGPASSWORD", "PGPASSWORD=pgpw psql -h db", []string{"pgpw"}},
	{"python inline", `python -c "import requests; requests.get('https://x', auth=('u','PyPw55'))"`, []string{"PyPw55"}},
	{"node inline", "node -e console.log(process.env.X||'NodePw6')", []string{"NodePw6"}},
	{"url fragment token", "open https://app.x/#access_token=FragTok77", []string{"FragTok77"}},
	{"ftp url", "curl ftp://user:FtpPw88@ftp.x/file", []string{"FtpPw88"}},
	{"slack webhook", "curl -X POST https://hooks.slack.com/services/T000/B000/XXXXsecretXXXX", []string{"XXXXsecretXXXX"}},
	{"aws cli key-value", "aws configure set aws_access_key_id AKIAEXAMPLE123", []string{"AKIAEXAMPLE123"}},
	{"tab separated secret", "mysql\t-p\tTabPw77", []string{"TabPw77"}},
	{"curly single quotes", "curl -d ‘pw=CurlyPw1’ https://x", []string{"CurlyPw1"}},
	{"control characters around a secret", "echo \x1b]52;c;U2VjcmV0Q2xpcAo=\x07 && echo ok", []string{"U2VjcmV0Q2xpcAo="}},
	{"basic auth header bare", "curl -H Authorization:Basic_YWxhZGRpbjpvcGVu https://x", []string{"YWxhZGRpbjpvcGVu"}},
	{"here-doc into an interpreter", "python - <<'PY'\nprint('PyHeredocPw')\nPY", []string{"PyHeredocPw"}},
	{"ps here-string", "$s = @'\nHereStrPw9\n'@\nSet-Content a.txt $s", []string{"HereStrPw9"}},
	{"secret in path-like argument", "cat /tmp/SECRETPATHpw11/x", []string{"SECRETPATHpw11"}},
	{"wrapper then secret", "sudo -u root env TOK=SudoTok1 tool", []string{"SudoTok1"}},
	{"xargs secret", "echo XargsPw2 | xargs -I{} curl -u u:{} https://x", []string{"XargsPw2"}},

	// Round 3: command-word-shaped secrets the round-2 critic found.
	{"secret as later command word after a bare -p", "mysql -u root -p ; hunter2pw48", []string{"hunter2pw48"}},
	{"secret on the line after a bare -p", "mysql -u root -p\nlinetwopw51", []string{"linetwopw51"}},
	{"mixed-case command word", "ls | CorrectHorseBatteryStaple", []string{"CorrectHorseBatteryStaple"}},
	{"first command word with digits inside", "s3cr3tfirst49", []string{"s3cr3tfirst49"}},
	{"first command word mixed and digits", "Sup3rS3cretFirst50 ; tq allow acme", []string{"Sup3rS3cretFirst50"}},
	{"value after $x =, cmdlet-shaped", "$pw = Hunter-Twopass; Do-Thing", []string{"Hunter-Twopass"}},
	{"long flag with a digit inside", "curl --abcdef64secret https://x", []string{"abcdef64secret"}},
	{"path-shaped command word", "/tmp/abc/s3cr3tpath66 run", []string{"s3cr3tpath66"}},
	{"sudo then a secret", "sudo sudopass67", []string{"sudopass67"}},
	{"xargs then a secret", "xargs xargspw58", []string{"xargspw58"}},
	{"timeout then a secret", "timeout 5 zz9secret68", []string{"zz9secret68"}},
	{"verb-shaped secret in a here-doc body", "cat > notes.txt <<EOF\nverbshapedsecret\nEOF\nls", []string{"verbshapedsecret"}},
	{"verb-shaped secret in a dash here-doc", "cat <<-END\n\tanotherverbsecret --flag\n\tEND", []string{"anotherverbsecret"}},
}

// The subject (the verb the guard read plus the matched path or token) goes
// through the same rule: a secret that stands where the verb is never
// reaches the subject either (CommandWord).
func TestCommandWordMasksSecretShapedVerbs(t *testing.T) {
	for _, w := range []string{"Sup3rS3cretFirst50", "sudopass67", "xargspw58", "zz9secret68", "s3cr3tfirst49", "CorrectHorseBatteryStaple"} {
		if got := CommandWord(w); got != "…" {
			t.Errorf("%q kept as %q", w, got)
		}
	}
	for _, w := range []string{"git", "rm", "tq", "Remove-Item", "npm", ""} {
		if got := CommandWord(w); got != w {
			t.Errorf("%q became %q", w, got)
		}
	}
}

func TestRedactCommandLeakCorpus(t *testing.T) {
	leaks := 0
	for _, c := range leakCorpus {
		got := RedactCommand(c.cmd)
		for _, s := range c.secrets {
			if strings.Contains(got, s) {
				leaks++
				t.Errorf("%s: %q still carries %q", c.name, got, s)
			}
		}
	}
	if leaks > 0 {
		t.Errorf("%d leaks in %d commands", leaks, len(leakCorpus))
	}
}

// What stays: the shape a human needs to tell what was tried.
func TestRedactCommandKeepsTheShape(t *testing.T) {
	for in, want := range map[string]string{
		"git push origin main":                   "git push origin main",
		"aws ec2 run-instances --image-id ami-1": "aws ec2 run-instances --image-id …",
		"rm -rf ~/.tentaqles/config.yaml":        "rm -r… …",
		"GH_TOKEN=x gh pr create":                "GH_TOKEN=… gh pr create",
		"cat .env | grep KEY":                    "cat … | grep …",
		`git commit -m "fix: x"`:                 `git commit -m "…"`,
		"npm publish --token=abc":                "npm publish --token=…",
		"echo $TOKEN && printenv":                "echo $TOKEN && printenv",
		"Get-ChildItem env: -Recurse":            "Get-ChildItem … -Recurse",
		"terraform apply -auto-approve":          "terraform apply -auto-approve",
	} {
		if got := RedactCommand(in); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
	// The words the deny matched (its subject) are kept.
	if got := RedactCommand("rm -rf ~/.tentaqles/config.yaml", "~/.tentaqles/config.yaml"); got != "rm -r… ~/.tentaqles/config.yaml" {
		t.Errorf("subject kept: %q", got)
	}
}

func TestRedactCommandStripsControlCharacters(t *testing.T) {
	got := RedactCommand("echo \x1b]0;pwned\x07\x1b[2K\x1b[1Afake\r line\x9b2J")
	for _, c := range got {
		if c < 0x20 && c != '\n' || c == 0x7f || (c >= 0x80 && c < 0xa0) {
			t.Fatalf("control character %U in %q", c, got)
		}
	}
}
