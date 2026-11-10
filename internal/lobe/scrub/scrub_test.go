package scrub

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Secrets in these tests are assembled from pieces, as the sources assemble theirs,
// so this file holds no live-looking key.
const (
	b62    = "A1b2C3d4E5f6G7h8J9k0L1m2N3p4Q5r6S7t8"
	hex64  = "3f2a9c1e5b7d4f6a8c0e2b4d6f8a0c2e4b6d8f0a1c3e5a7b9d1f3a5c7e9b1d3f"
	dirD62 = "/var/folders/q_/cvygw2vb7rq41f7mdic22vap0000gn/T/"
)

// parityCases are the inputs the source test files expect to be caught, with the
// part of each that must not survive. Each row names its source file and line.
var parityCases = []struct{ input, secret string }{
	// privacy.test.ts
	{"api_key=sk-ant-1234567890abcdefghij", "1234567890abcdefghij"},                                                                    // privacy.test.ts:31
	{"token: ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefgh", "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefgh"},                                            // privacy.test.ts:38
	{"found ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij here", "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij"},                                    // privacy.test.ts:44
	{"xoxb-123456-789012-abcdef", "123456-789012-abcdef"},                                                                              // privacy.test.ts:49
	{"key=AKIAIOSFODNN7EXAMPLE", "AKIAIOSFODNN7EXAMPLE"},                                                                               // privacy.test.ts:55
	{"eyJhbGciOiJIUzI1.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIn0.SflKxwRJSMeKKF2QT4fwpM", "SflKxwRJSMeKKF2QT4fwpM"},          // privacy.test.ts:63
	{"sk-1234567890abcdefghijklmnopqr", "1234567890abcdefghijklmnopqr"},                                                                // privacy.test.ts:69
	{"sk-proj-1234567890abcdefghijklmnopqrstuvwxyzABCDEFGHIJ", "1234567890abcdefghijklmnopqrstuvwxyzABCDEFGHIJ"},                       // privacy.test.ts:76
	{"ghs_1234567890abcdefghijklmnopqrstuvwxyzAB", "1234567890abcdefghijklmnopqrstuvwxyzAB"},                                           // privacy.test.ts:82
	{"Authorization: Bearer sk-proj-1234567890abcdefghijklmnopqrstuvwxyzABCDEFGHIJ", "1234567890abcdefghijklmnopqrstuvwxyzABCDEFGHIJ"}, // privacy.test.ts:89
	{"sk-abcdefghijklmnopqrstuv and ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij", "abcdefghijklmnopqrstuv"},                               // privacy.test.ts:96
	{"sk-ABCDEFGHIJKLMNOPQRSTUVWXYZabc", "ABCDEFGHIJKLMNOPQRSTUVWXYZabc"},                                                              // privacy.test.ts:117
	{"sk-abcdefghijklmnopqrstuvwxyz", "abcdefghijklmnopqrstuvwxyz"},                                                                    // privacy.test.ts:141
	{"pk-abcdefghijklmnopqrstuvwxyz", "abcdefghijklmnopqrstuvwxyz"},                                                                    // privacy.test.ts:141
	{"rk-abcdefghijklmnopqrstuvwxyz", "abcdefghijklmnopqrstuvwxyz"},                                                                    // privacy.test.ts:141
	{"ak-abcdefghijklmnopqrstuvwxyz", "abcdefghijklmnopqrstuvwxyz"},                                                                    // privacy.test.ts:141
	{"sk-proj-abcdefghijklmnopqrstuvwxyz", "abcdefghijklmnopqrstuvwxyz"},                                                               // privacy.test.ts:141
	{"sk-ant-abcdefghijklmnopqrstuvwxyz", "abcdefghijklmnopqrstuvwxyz"},                                                                // privacy.test.ts:141
	{"(sk-abcdefghijklmnopqrstuvwxyz)", "abcdefghijklmnopqrstuvwxyz"},                                                                  // privacy.test.ts:142 (punctuation boundary)
	{"\U0001F511sk-abcdefghijklmnopqrstuvwxyz!", "abcdefghijklmnopqrstuvwxyz"},                                                         // privacy.test.ts:142 (emoji boundary)

	// trail-secrets.test.ts: the `secrets` table
	{"sk-proj-" + "Ab3dE5fG7hJ9kL1mN3pQ5rS7tU9vW1xY3z", "Ab3dE5fG7hJ9kL1mN3pQ5rS7tU9vW1xY3z"},  // trail-secrets.test.ts:17
	{"sk-ant-api03-" + "Zx9Yw8Vu7Ts6Rq5Po4Nm3Lk2Jh1Gf0Ed", "Zx9Yw8Vu7Ts6Rq5Po4Nm3Lk2Jh1Gf0Ed"}, // trail-secrets.test.ts:18
	{"gh" + "p_" + b62, b62}, // trail-secrets.test.ts:19
	{"gh" + "o_" + b62, b62}, // trail-secrets.test.ts:20
	{"github" + "_pat_" + "11ABCDEFG0" + "abcdefghijklmn" + "_" + b62 + b62, "11ABCDEFG0"},                // trail-secrets.test.ts:21
	{"xo" + "xb-" + "1234567890-1234567890123-" + "AbCdEfGhIjKlMnOpQrStUvWx", "AbCdEfGhIjKlMnOpQrStUvWx"}, // trail-secrets.test.ts:22
	{"xo" + "xp-" + "1234567890-1234567890123-" + "AbCdEfGhIjKlMnOpQrStUvWx", "AbCdEfGhIjKlMnOpQrStUvWx"}, // trail-secrets.test.ts:23
	{"AK" + "IA" + "Z7Q2X9W4V1T8R3S6", "Z7Q2X9W4V1T8R3S6"},                                                // trail-secrets.test.ts:24
	{"np" + "m_" + b62, b62}, // trail-secrets.test.ts:25
	{"AI" + "za" + "Sy" + "D1f2G3h4J5k6L7m8N9p0Q1r2S3t4U5v", "SyD1f2G3h4J5k6L7m8N9p0Q1r2S3t4U5v"}, // trail-secrets.test.ts:26
	{"s" + "k_live_" + "4eC39HqLyjWDarjtT1zdp7dc", "4eC39HqLyjWDarjtT1zdp7dc"},                    // trail-secrets.test.ts:27
	{"n" + "n" + hex64, hex64},                                    // trail-secrets.test.ts:28
	{"m" + "b_" + "a1B2c3D4e5F6g7H8i9J0", "a1B2c3D4e5F6g7H8i9J0"}, // trail-secrets.test.ts:29
	{"ke" + "y-" + hex64[:32], hex64[:32]},                        // trail-secrets.test.ts:30
	{"curl -H \"X-Api-Key: " + "tr" + "l_" + "Q7mZp3Lx9KwR2vT8nB5y" + "\" https://api.example.test/v1", "Q7mZp3Lx9KwR2vT8nB5y"}, // trail-secrets.test.ts:31
	{"TRAIL_API_KEY=abc123def456ghi789", "abc123def456ghi789"},                                                                  // trail-secrets.test.ts:32
	{"export DB_PASSWORD='s3cr3t-Pa55'", "s3cr3t-Pa55"},                                                                         // trail-secrets.test.ts:33
	{"token: 9f8e7d6c5b4a", "9f8e7d6c5b4a"},                                                                                     // trail-secrets.test.ts:34
	{"{\"client_secret\": \"Zx9-kQ2-pL7\"}", "Zx9-kQ2-pL7"},                                                                     // trail-secrets.test.ts:35
	{"password: hunter2", "hunter2"},                                                                                            // trail-secrets.test.ts:36
	{"API_KEY=123e4567-e89b-12d3-a456-426614174000", "123e4567-e89b-12d3-a456-426614174000"},                                    // trail-secrets.test.ts:37
	{"deploy --password Tr4ck3r99x", "Tr4ck3r99x"},                                                                              // trail-secrets.test.ts:38
	{"curl -H 'Authorization: Bearer " + "abcDEF" + "123456ghiJKL789" + "' https://api.example.test", "abcDEF123456ghiJKL789"},  // trail-secrets.test.ts:39
	{"Authorization: Basic dXNlcjpwYXNzd29yZA==", "dXNlcjpwYXNzd29yZA=="},                                                       // trail-secrets.test.ts:40
	{"-----BEGIN RSA PRIVATE KEY-----\nMIIEpAIBAAKCAQEA\n-----END RSA PRIVATE KEY-----", "MIIEpAIBAAKCAQEA"},                    // trail-secrets.test.ts:41
	{"-----BEGIN OPENSSH PRIVATE KEY-----", "OPENSSH"},                                                                          // trail-secrets.test.ts:42
	{"-----BEGIN EC PRIVATE KEY-----", "EC PRIVATE"},                                                                            // trail-secrets.test.ts:43
	{"-----BEGIN PRIVATE KEY-----", "PRIVATE KEY"},                                                                              // trail-secrets.test.ts:44
	{"-----BEGIN PGP PRIVATE KEY BLOCK-----", "PGP"},                                                                            // trail-secrets.test.ts:45
	{"ey" + "JhbGciOiJIUzI1NiJ9" + "." + "eyJzdWIiOiIxMjM0NTY3ODkwIn0" + "." + "dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U", "dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U"}, // trail-secrets.test.ts:46
	{"psql postgres://admin:Hx7rT2pq@db.example.test:5432/app", "Hx7rT2pq"},                                                                                                  // trail-secrets.test.ts:47
	{"curl -u deploy:Tr4ck3r99 https://ci.example.test/api", "Tr4ck3r99"},                                                                                                    // trail-secrets.test.ts:48
	{"Zq8+Xv3Lp9nRt2W" + "m5Kd7Fh1Bs4Gj6Hc0Ny8Ue3Ao", "Zq8+Xv3Lp9nRt2W"},                                                                                                     // trail-secrets.test.ts:49
	{"Q7mZp3Lx9KwR2vT8" + "nB5yH1cF6dJ4gS0a", "Q7mZp3Lx9KwR2vT8"},                                                                                                            // trail-secrets.test.ts:50
	{"curl 'https://maps.example.test/api/geocode?address=x&key=" + "Q7mZp3Lx9KwR2vT8nB5yH1cF6dJ4gS0a" + "'", "Q7mZp3Lx9KwR2vT8nB5yH1cF6dJ4gS0a"},                            // trail-secrets.test.ts:51
	{"curl -O 'https://bucket.example.test/f.zip?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Signature=" + hex64 + "'", hex64},                                                    // trail-secrets.test.ts:52
	{"curl -X POST https://hooks.slack.com/services/" + "T0A1B2C3D/B0A1B2C3D/" + "abcDEF123ghiJKL456mnoPQR", "T0A1B2C3D"},                                                    // trail-secrets.test.ts:53
	{"https://discord.com/api/webhooks/123456789012345678/" + "AbCdEf0123456789-AbCdEf0123456789_AbCdEf0123456789AbCdEf0123456789ab", "AbCdEf0123456789-AbCdEf"},             // trail-secrets.test.ts:54
	{"curl https://api.telegram.org/bot" + "123456789:" + "AA" + "Hx9TcvQ1vGWJxfSeofSAs0K5PALDzz" + "/getMe", "Hx9TcvQ1vGWJxfSeofSAs0K5PALDzz"},                              // trail-secrets.test.ts:55
	{"sb_" + "secret_" + "A1b2C3d4E5f6G7h8J9k0L1m2N3p4Q5r", "A1b2C3d4E5f6G7h8J9k0L1m2N3p4Q5r"},                                                                               // trail-secrets.test.ts:56
	{"GOC" + "SPX-" + "A1b2C3d4E5f6G7h8J9k0L1m2N3p4", "A1b2C3d4E5f6G7h8J9k0L1m2N3p4"},                                                                                        // trail-secrets.test.ts:57
	{"PGPASSWORD=shop_dev_pass psql -h db.example.test", "shop_dev_pass"},                                                                                                    // trail-secrets.test.ts:58
	{"password: P@ss(w0rd)!", "P@ss"}, // trail-secrets.test.ts:59
	{"curl 'https://api.example.test/v1/me?access_token=" + "EA" + "Ab1C2d3E4f5G6h7I8j9K0l1M2n3O4p5Q6r" + "&fields=id'", "Ab1C2d3E4f5G6h7I8j9K0l1M2n3O4p5Q6r"}, // trail-secrets.test.ts:60
	{"TOKEN=\"" + "EA" + "Ab1C2d3E4f5G6h7I8j9K0l1M2n3O4p5Q6rS7t8U9v0" + "\"", "Ab1C2d3E4f5G6h7I8j9K0l1M2n3O4p5Q6rS7t8U9v0"},                                    // trail-secrets.test.ts:61

	// trail-secrets.test.ts: the D62 temp-folder cases
	{dirD62 + "gh" + "p_" + b62, b62}, // trail-secrets.test.ts:195
	{"cat /private" + dirD62 + "mb_a1B2c3D4e5F6g7H8i9J0" + ".json", "a1B2c3D4e5F6g7H8i9J0"}, // trail-secrets.test.ts:196
	{"ls " + dirD62 + b62, b62}, // trail-secrets.test.ts:197
	{"TOKEN=" + "Zq8Xv3Lp9n" + "Rt2Wm5Kd7F" + " node " + dirD62 + "x-AbCdEf/run.js", "Zq8Xv3Lp9nRt2Wm5Kd7F"},     // trail-secrets.test.ts:198
	{"psql postgres://app:" + "hunter" + "42xyz" + "@db.example.test/app -f " + dirD62 + "q.sql", "hunter42xyz"}, // trail-secrets.test.ts:199
}

// parityNotPorted lists source rows that cannot or should not be caught under the
// specification, each with a reason. It is empty: every source row is caught.
var parityNotPorted = []struct{ input, reason string }{}

// cleanCases are the inputs the source test files expect to come back unchanged, each with its
// source file and line. Rows that fail only for a deliberate difference of this design are in
// cleanNotPorted, not here.
var cleanCases = []struct{ desc, text string }{
	// trail-secrets.test.ts
	{"a git sha", "git checkout 3f2a9c1e5b7d4f6a8c0e2b4d6f8a0c2e4b6d8f0a"},                                        // trail-secrets.test.ts:65
	{"a short sha range", "git log a1b2c3d..e4f5a6b"},                                                             // trail-secrets.test.ts:66
	{"an image digest", "docker pull registry.example.test/shop@sha256:" + hex64},                                 // trail-secrets.test.ts:67
	{"an uppercase hex checksum", "echo 9F86D081884C7D659A2FEAA0C55AD015A3BF4F1B2B0B822CD15D6C15B0F00A08"},        // trail-secrets.test.ts:68
	{"a version", "npm version 0.22.1 && git tag v0.22.1"},                                                        // trail-secrets.test.ts:69
	{"a pre-release version", "npm publish --tag next 1.4.0-beta.2+build.15"},                                     // trail-secrets.test.ts:70
	{"a UUID as an id", "trail runs show 123e4567-e89b-12d3-a456-426614174000"},                                   // trail-secrets.test.ts:71
	{"a UUID in a path", "cat /tmp/sessions/123E4567-E89B-12D3-A456-426614174000/Run.json"},                       // trail-secrets.test.ts:72
	{"an uppercase UUID between short path pieces", "cat /srv/v1/9F8E7D6C-5B4A-3C2D-1E0F-A9B8C7D6E5F4/a.json"},    // trail-secrets.test.ts:73
	{"a lowercase UUID in a long path", "ls /home/dev/.trail/sessions/123e4567-e89b-12d3-a456-426614174000.json"}, // trail-secrets.test.ts:74
	{"a source path", "vim /home/dev/shop/src/server/routes/v1/AdminController.ts"},                               // trail-secrets.test.ts:75
	{"a placeholder in an export", "export DB_PASSWORD=$DB_PASSWORD"},                                             // trail-secrets.test.ts:76
	{"a braced placeholder", "API_KEY=${API_KEY:?set API_KEY}"},                                                   // trail-secrets.test.ts:77
	{"an angle placeholder", "TOKEN=<token>"},                                                                     // trail-secrets.test.ts:78
	{"a bearer placeholder", `curl -H "Authorization: Bearer $GITHUB_TOKEN" https://api.example.test`},            // trail-secrets.test.ts:79
	{"masked", "password: ***"},                                                                          // trail-secrets.test.ts:80
	{"an example value", "API_KEY=your_api_key_here"},                                                    // trail-secrets.test.ts:81
	{"changeme", "SECRET=changeme"},                                                                      // trail-secrets.test.ts:82
	{"a token count", "MAX_TOKENS=4096 max_tokens: 8000"},                                                // trail-secrets.test.ts:84
	{"a token lifetime", "TOKEN_TTL=3600"},                                                               // trail-secrets.test.ts:85
	{"a password rule", "PASSWORD_MIN_LENGTH=12"},                                                        // trail-secrets.test.ts:86
	{"a reference to the environment", "API_KEY = process.env.API_KEY"},                                  // trail-secrets.test.ts:87
	{"a Python lookup", "token=os.environ['GH_TOKEN']"},                                                  // trail-secrets.test.ts:88
	{"plain commands", "npm publish --access public && git push origin main && gh pr checks 42 --watch"}, // trail-secrets.test.ts:91
	{"a long identifier", "getS3BucketPolicyForAccountV2AndRegion"},                                      // trail-secrets.test.ts:92
	{"a library name", "pip install sk-learn-preprocessing-pipeline"},                                    // trail-secrets.test.ts:93
	{"a dotted class name", "java -cp app.jar com.example.app.v2.PaymentService3Impl"},                   // trail-secrets.test.ts:94
	{"a pod name", "kubectl logs web-7d4b9c8f6d-x2k9p"},                                                  // trail-secrets.test.ts:95
	{"a timestamp", "since 2026-10-15T10:00:00.000Z"},                                                    // trail-secrets.test.ts:96
	{"API object ids", "curl https://api.example.test/v1/deployments?teamId=team_a1B2c3D4e5F6g7H8i9J0 && stripe customers retrieve cus_Q7mZp3Lx9KwR2vT8nB"}, // trail-secrets.test.ts:97
	{"a one-time code flag", "npm publish --otp=123456"}, // trail-secrets.test.ts:98
	{"empty", ""}, // trail-secrets.test.ts:99
	{"a sentence about secrets", "Never commit the .env file; the API key lives in the keychain."},                                                           // trail-secrets.test.ts:100
	{"a token read from a command", `TOKEN=$(gh auth token) && curl -H "Authorization: Bearer $TOKEN" https://api.example.test`},                             // trail-secrets.test.ts:102
	{"a password read from the keychain", "PGPASSWORD=$(security find-generic-password -s db -w) psql -h db.example.test"},                                   // trail-secrets.test.ts:103
	{"a key read with backticks", "API_KEY=`cat /home/dev/.config/shop/key` npm run sync"},                                                                   // trail-secrets.test.ts:104
	{"a bearer read from a script", `curl -H "Authorization: Bearer $(python3 mint.py)" https://api.example.test`},                                           // trail-secrets.test.ts:105
	{"a length check", `echo "secret_present=${#SECRET}"`},                                                                                                   // trail-secrets.test.ts:106
	{"a printf format", `printf 'API_KEY=%s\n' "$KEY" >> .env`},                                                                                              // trail-secrets.test.ts:107
	{"a curl write-out format", `curl -s -o /dev/null -w 'no-token=%{http_code}' https://api.example.test`},                                                  // trail-secrets.test.ts:108
	{"an elided key", "export TRAIL_API_KEY=nn... and run trail init"},                                                                                       // trail-secrets.test.ts:109
	{"another variable's name", `echo "api_key_rejected=VENDOR_API_KEY"`},                                                                                    // trail-secrets.test.ts:112
	{"a sed that redacts", `sed -E 's/password=.*/password=***/' app.ini`},                                                                                   // trail-secrets.test.ts:114
	{"a grep for bearer tokens", `grep -oE 'Bearer [A-Za-z0-9._-]+' access.log`},                                                                             // trail-secrets.test.ts:115
	{"a grep for npm tokens", `grep -E '_authToken=(.+)' /home/dev/.npmrc`},                                                                                  // trail-secrets.test.ts:116
	{"a regex capture", `m=re.search(r'access_token=([^&]*)', url)`},                                                                                         // trail-secrets.test.ts:117
	{"a schema", "const env = z.object({ OPENAI_API_KEY: z.string().min(1) })"},                                                                              // trail-secrets.test.ts:118
	{"a generated id", "const trackingToken = crypto.randomUUID()"},                                                                                          // trail-secrets.test.ts:119
	{"a comparison", "if (process.env.TRAIL_API_KEY === undefined) throw new Error('missing')"},                                                              // trail-secrets.test.ts:121
	{"an escaped newline", `python3 -c "self.token = None\nprint(1)"`},                                                                                       // trail-secrets.test.ts:122
	{"a UTC timestamp", "date -u +%Y-%m-%dT%H:%M:%SZ"},                                                                                                       // trail-secrets.test.ts:124
	{"a quoted UTC time", `NOW=$(date -u "+%H:%M:%S")`},                                                                                                      // trail-secrets.test.ts:125
	{"a user and group id", "docker run --rm -u 1000:1000 -v /home/dev/shop:/app shop:dev npm test"},                                                         // trail-secrets.test.ts:126
	{"a login read at run time", `curl -u "deploy:$(cat /home/dev/.config/ci-pass)" https://ci.example.test/api`},                                            // trail-secrets.test.ts:127
	{"a slice by a name's length", `const v = raw.slice("TRAIL_API_KEY=".length)`},                                                                           // trail-secrets.test.ts:130
	{"a call", "const secret = secretFor(`${repo}`)"},                                                                                                        // trail-secrets.test.ts:131
	{"a call with arguments", "const trackingToken = crypto.randomBytes(16).toString('hex')"},                                                                // trail-secrets.test.ts:132
	{"a grep for a key shape", `grep -E 'Bearer nn[a-f0-9]{64}' access.log`},                                                                                 // trail-secrets.test.ts:133
	{"a document link without its scheme", "see docs.example.test/d/1Bx7kQ2_mZp3Lx9KwR2vT8nB5yH1cF6dJ4gS0a-Zq8Xv3/edit"},                                     // trail-secrets.test.ts:134
	{"a sha in a variable", "BASE=" + hex64[:40] + " && git diff $BASE..HEAD"},                                                                               // trail-secrets.test.ts:136
	{"an image file on a CDN", "curl -O https://cdn.example.test/user_2abcDEF3ghiJKL4mnoPQR5stuVW/hf_20261015_a1b2c3d4.png"},                                 // trail-secrets.test.ts:137
	{"a deployment link", "curl https://api.example.test/v13/deployments/dpl_8xKfR2mNpQ7sT4vW9yZ1aB3cD5eF"},                                                  // trail-secrets.test.ts:138
	{"a document link", "open https://docs.example.test/spreadsheets/d/1GAbCdEfGhIjKlMnOpQrStUvWxYz0123456789AbCdEf/edit"},                                   // trail-secrets.test.ts:139
	{"a share link", "https://share.example.test/artifact/Azy3kLm9PqR2sT8vW1xY4z and https://share.example.test/s/Q7mZp3Lx9KwR2vT8nB5yH1cF6dJ4gS0aZq8Xv3Lp"}, // trail-secrets.test.ts:140
	{"a click id", "https://shop.example.test/page?fbclid=IwY2xjawF0aBcDeFgHiJkLmNoPqRsTuVwXyZ0123456789AbCdEfGhIjKlMnOpQrStUv"},                             // trail-secrets.test.ts:141
	{"a temp folder", "ls /var/folders/rz/4bn1x2y3z4a5b6c7d8e9f0g1h2i3j4/T/"},                                                                                // trail-secrets.test.ts:142
	{"an account id with a hex body", "ACCOUNT=AC" + hex64[:32] + " && echo $ACCOUNT"},                                                                       // trail-secrets.test.ts:143
	{"a product id", "billing products get pdt_A1b2C3d4E5f6G7h8J9k0L"},                                                                                       // trail-secrets.test.ts:144
	{"an id named after a key", `{"APIKeyID": "k-2026-10-15", "tokenUrl": "https://auth.example.test/t0k3n"}`},                                               // trail-secrets.test.ts:145
	// privacy.test.ts: the rows that come back unchanged
	{"a task name with a long dashed form", "task-runner-that-does-something-long"}, // privacy.test.ts:124
	{"a bookmark name", "bookmark-this-long-reference-identifier"},                  // privacy.test.ts:125
	{"a break name", "break-this-long-background-operation"},                        // privacy.test.ts:126
	{"a task-proj identifier", "task-proj-abcdefghijklmnopqrstuvwxyz"},              // privacy.test.ts:127
	{"a task-ant identifier", "task-ant-abcdefghijklmnopqrstuvwxyz"},                // privacy.test.ts:128
	{"a prefix_pk identifier", "prefix_pk-abcdefghijklmnopqrstuvwxyz"},              // privacy.test.ts:129
	{"a digit before rk", "42rk-abcdefghijklmnopqrstuvwxyz"},                        // privacy.test.ts:130
	{"a letter with a diacritic before ak", "éak-abcdefghijklmnopqrstuvwxyz"},       // privacy.test.ts:131
	{"a CJK letter before sk", "漢sk-abcdefghijklmnopqrstuvwxyz"},                    // privacy.test.ts:132
	{"a combining mark before sk", "ésk-abcdefghijklmnopqrstuvwxyz"},               // privacy.test.ts:133
}

// cleanNotPorted are clean rows of the source files that this design does not pass, each with the one
// deliberate reason. They are not tested.
var cleanNotPorted = []struct{ text, reason string }{
	// trail-secrets.test.ts:83
	{"token: always in the keychain", "letters-only value under 16 characters: Trail's plain word, deliberately absent"},
	// trail-secrets.test.ts:120 (the value is read as `string):`, a letters-only word)
	{"function sign(token: string): void {}", "letters-only value under 16 characters: Trail's plain word, deliberately absent"},
	// trail-secrets.test.ts:129 (the value is `_omit`)
	{"const { OPENAI_API_KEY: _omit, ...rest } = env", "letters-only value under 16 characters: Trail's plain word, deliberately absent"},
	// trail-secrets.test.ts:89
	{"psql postgres://postgres:postgres@localhost:5432/app", "we redact every URL login password: a local port can be a tunnel to a real database"},
	// trail-secrets.test.ts:90
	{"psql postgres://app:Hx7rT2pq@127.0.0.1:5432/app", "we redact every URL login password: a local port can be a tunnel to a real database"},
	// trail-secrets.test.ts:110
	{`SUPABASE_SECRET_KEY="sb_secret_placeholder"`, "a value that says it is a stand-in in words: not a placeholder here, deliberately absent"},
	// trail-secrets.test.ts:111
	{"GEMINI_API_KEY=fake-key-for-tests npm test", "a value that says it is a stand-in in words: not a placeholder here, deliberately absent"},
	// trail-secrets.test.ts:128
	{"OPENAI_API_KEY=sk-test npm test && curl -H 'Authorization: Bearer test-key' localhost:3000 && API_KEY=invalid_key_for_401 npm run check", "stand-in words (test, invalid): not placeholders here, deliberately absent"},
}

// TestLobeScrubParity: every source row that expects a secret is redacted, and the secret is gone.
func TestLobeScrubParity(t *testing.T) {
	for _, c := range parityCases {
		out := Text(c.input).String()
		if !strings.Contains(out, "[redacted:") {
			t.Errorf("not redacted: %q\n got: %q", c.input, out)
			continue
		}
		if strings.Contains(out, c.secret) {
			t.Errorf("secret %q survives in %q\n got: %q", c.secret, c.input, out)
		}
	}
}

func TestLobeScrubShortValue(t *testing.T) {
	if got := Text("password=hunter2").String(); got != "password=[redacted:named]" {
		t.Fatalf("got %q", got)
	}
}

func TestLobeScrubNoFalseAlarm(t *testing.T) {
	for _, s := range []string{
		"3f2a9c1e5b7d4a6f8e0c1b2d3a4f5e6d7c8b9a0f",
		"123e4567-e89b-12d3-a456-426614174000",
		"MAX_TOKENS=4096",
		"export API_KEY=$TOKEN",
		`C:\Users\dev\project\src\auth\token_store.go`,
		"/home/dev/project/src/auth/token_store.go",
		"git show 3f2a9c1e5b7d4a6f8e0c1b2d3a4f5e6d7c8b9a0f && echo MAX_TOKENS=4096",
		"pip install sk-learn-preprocessing-pipeline",
	} {
		if got := Text(s).String(); got != s {
			t.Errorf("changed a plain text:\n in:  %q\n out: %q", s, got)
		}
	}
}

// ordinaryWords is n characters of plain words.
func ordinaryWords(n int) string {
	return strings.Repeat("ordinary words in a long line ", n/30+1)[:n]
}

// timingInput is a full-size tool input and error, as the hook scrubs them.
func timingInput(t testing.TB) (json.RawMessage, string) {
	raw, err := json.Marshal(map[string]string{"note": ordinaryWords(4256)})
	if err != nil {
		t.Fatal(err)
	}
	return raw, ordinaryWords(8000)
}

// TestLobeScrubTiming: a full-size input and error scrub in at most 20 ms on average (target 5 ms). It
// times five batches of 20 runs and keeps the fastest, so other test packages running on the same machine
// cannot push one slow batch over the limit; a scrubber that is really slow is slow in every batch.
func TestLobeScrubTiming(t *testing.T) {
	raw, errText := timingInput(t)
	const runs, batches = 20, 5
	best := time.Duration(1<<63 - 1)
	for b := 0; b < batches; b++ {
		start := time.Now()
		for i := 0; i < runs; i++ {
			_ = Input(raw)
			_ = Text(errText)
		}
		best = min(best, time.Since(start)/runs)
	}
	t.Logf("Input plus Text, average over %d runs, fastest of %d batches: %v", runs, batches, best)
	if best > 20*time.Millisecond {
		t.Fatalf("too slow: %v on average (target 5 ms)", best)
	}
}

// coldScrubVar names the helper run of TestLobeScrubColdStart.
const coldScrubVar = "LOBE_SCRUB_COLD"

// TestLobeScrubColdStart: the first scrub of a process, which builds every pattern, takes at most 20 ms.
// A hook is a new process on every call, so it pays this cost each time. The test runs itself again as a
// fresh process (TestLobeScrubColdHelper), three times, and keeps the fastest.
func TestLobeScrubColdStart(t *testing.T) {
	if os.Getenv(coldScrubVar) != "" {
		t.Skip("helper run")
	}
	best := time.Duration(1<<63 - 1)
	for i := 0; i < 3; i++ {
		cmd := exec.Command(os.Args[0], "-test.run=^TestLobeScrubColdHelper$", "-test.count=1")
		cmd.Env = append(os.Environ(), coldScrubVar+"=1")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("helper run failed: %v\n%s", err, out)
		}
		_, after, found := strings.Cut(string(out), "cold-ns=")
		if !found {
			t.Fatalf("helper printed no time:\n%s", out)
		}
		ns, err := strconv.ParseInt(strings.Fields(after)[0], 10, 64)
		if err != nil {
			t.Fatalf("helper time %q: %v", after, err)
		}
		best = min(best, time.Duration(ns))
	}
	t.Logf("first Input plus Text of a process, fastest of 3: %v", best)
	if best > 20*time.Millisecond {
		t.Fatalf("first scrub too slow: %v (limit 20 ms)", best)
	}
}

// TestLobeScrubColdHelper is not a test: TestLobeScrubColdStart runs it in a fresh process, where it
// times the first scrub and prints the time. Without the variable it returns at once.
func TestLobeScrubColdHelper(t *testing.T) {
	if os.Getenv(coldScrubVar) == "" {
		return
	}
	raw, errText := timingInput(t)
	start := time.Now()
	_ = Input(raw)
	_ = Text(errText)
	fmt.Printf("cold-ns=%d\n", time.Since(start).Nanoseconds())
}

func TestLobeScrubWindowEdge(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 15; i++ {
		b.WriteString("ghp_" + strings.Repeat("A", 36) + " ")
	}
	b.WriteString(strings.Repeat("x", 1124))
	b.WriteString(" ")
	b.WriteString("ghp_" + strings.Repeat("B", 36))
	b.WriteString(strings.Repeat("x", 5000))
	got := Text(b.String()).String()
	if strings.Contains(got, "ghp_") || strings.Contains(got, "BBBB") {
		t.Errorf("a token survives: %q", got)
	}
	if runeLen(got) > 4000 {
		t.Errorf("longer than 4000: %d", runeLen(got))
	}
}

func TestLobeScrubWindow(t *testing.T) {
	e := strings.Repeat("x", 1000) + " ghp_" + strings.Repeat("A", 36) + " " + strings.Repeat("x", 5500)
	got := Text(e).String()
	if runeLen(got) > 4000 {
		t.Errorf("longer than 4000: %d", runeLen(got))
	}
	if !strings.Contains(got, "\n[...]\n") {
		t.Errorf("no [...] line: %q", got)
	}
	if !strings.Contains(got, "[redacted:api-key]") {
		t.Errorf("no api-key marker: %q", got)
	}
	if strings.Contains(got, "ghp_") {
		t.Errorf("token survives: %q", got)
	}
}

func TestLobeScrubInput(t *testing.T) {
	got := Input(json.RawMessage(`{"password":"hunter2","note":"hi","command":"deploy"}`)).String()
	if want := `{"command":"deploy","note":"hi","password":"[redacted:key]"}`; got != want {
		t.Errorf("got %q want %q", got, want)
	}

	cmd := `curl -H "Authorization: Bearer abcdefghijklmnopqrstuvwxyz012345" https://api.example.test/v1/items?api_key=Zx81kQpL0029aaBB`
	raw, err := json.Marshal(map[string]string{"command": cmd})
	if err != nil {
		t.Fatal(err)
	}
	got = Input(raw).String()
	for _, secret := range []string{"abcdefghijklmnopqrstuvwxyz012345", "Zx81kQpL0029aaBB"} {
		if strings.Contains(got, secret) {
			t.Errorf("secret %q survives: %q", secret, got)
		}
	}
	if !strings.Contains(got, "https://api.example.test/v1/items") {
		t.Errorf("the URL was lost: %q", got)
	}
	if strings.Count(got, "[redacted:") < 2 {
		t.Errorf("want two markers: %q", got)
	}

	if got := Input(json.RawMessage(`{"cmd":"a && b < c > d"}`)).String(); got != `{"cmd":"a && b < c > d"}` {
		t.Errorf("escaped characters: %q", got)
	}

	got = Input(json.RawMessage(`{"env":{"API_KEY":"abc12345"},"args":["--token","s3cr3tvalue99"]}`)).String()
	for _, secret := range []string{"abc12345", "s3cr3tvalue99"} {
		if strings.Contains(got, secret) {
			t.Errorf("nested secret %q survives: %q", secret, got)
		}
	}

	got = Input(json.RawMessage(`not json password=hunter2`)).String()
	var asString string
	if err := json.Unmarshal([]byte(got), &asString); err != nil {
		t.Errorf("not a JSON string: %q (%v)", got, err)
	}
	if strings.Contains(got, "hunter2") {
		t.Errorf("secret survives: %q", got)
	}
}

func TestLobeScrubText(t *testing.T) {
	got := Text("password=hunter2 rejected for postgres://app:s3cretPW@db.example.test:5432/main").String()
	for _, secret := range []string{"hunter2", "s3cretPW"} {
		if strings.Contains(got, secret) {
			t.Errorf("secret %q survives: %q", secret, got)
		}
	}
	if !strings.Contains(got, "db.example.test") {
		t.Errorf("host lost: %q", got)
	}
}

func TestLobeScrubMarkersAreNotSecrets(t *testing.T) {
	if kind, found := Detect("[redacted:api-key] and [redacted:named]"); found {
		t.Errorf("a marker reported as %s", kind)
	}
	for _, c := range parityCases {
		if kind, found := Detect(Text(c.input).String()); found {
			t.Errorf("scrubbed %q still detected as %s", c.input, kind)
		}
	}
}

func TestLobeScrubNoInit(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(src), "func init(") {
			t.Errorf("%s declares an init function", name)
		}
		// No package-level var may be initialized by a call: that would run code at start-up.
		f, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range f.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.VAR {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, v := range vs.Values {
					ast.Inspect(v, func(n ast.Node) bool {
						if call, ok := n.(*ast.CallExpr); ok {
							t.Errorf("%s: package-level var %s is initialized by a call at %s",
								name, vs.Names[0].Name, fset.Position(call.Pos()))
						}
						return true
					})
				}
			}
		}
	}
}

func TestLobeScrubKeyPass(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{`{"password":12345,"cmd":"deploy"}`, `{"cmd":"deploy","password":"[redacted:key]"}`},
		{`{"pwd":"hunter22","cmd":"deploy"}`, `{"cmd":"deploy","pwd":"[redacted:key]"}`},
		{`{"private_key":"abc","access_key":"def","auth_key":"ghi","n":1}`,
			`{"access_key":"[redacted:key]","auth_key":"[redacted:key]","n":1,"private_key":"[redacted:key]"}`},
		{`{"credentials":{"user":"bob","pass":"x1","port":5432,"tls":true,"note":null}}`,
			`{"credentials":{"note":null,"pass":"[redacted:key]","port":"[redacted:key]","tls":"[redacted:key]","user":"[redacted:key]"}}`},
		{`{"max_tokens":4096,"token_count":7,"cmd":"run"}`, `{"cmd":"run","max_tokens":4096,"token_count":7}`},
		{`{"secrets":["a1","b2"],"cmd":"x"}`, `{"cmd":"x","secrets":["[redacted:key]","[redacted:key]"]}`},
	} {
		if got := Input(json.RawMessage(c.in)).String(); got != c.want {
			t.Errorf("Input(%s)\n got:  %s\n want: %s", c.in, got, c.want)
		}
	}
}

func TestLobeScrubNamedValue(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"password: P@ss(w0rd)!", "password: [redacted:named]"},
		{"connect(password=abc12345)", "connect(password=[redacted:named])"},
		{`password: "correct horse battery"`, `password: "[redacted:named]"`},
		{"password='it is a secret' --verbose", "password='[redacted:named]' --verbose"},
		{`api_key=abc12345&user=bob`, `api_key=[redacted:named]&user=bob`},
		{"DB_PASSWORD=$DB_PASSWORD", "DB_PASSWORD=$DB_PASSWORD"},
		{"MAX_TOKENS=4096", "MAX_TOKENS=4096"},
	} {
		if got := Text(c.in).String(); got != c.want {
			t.Errorf("Text(%q)\n got:  %q\n want: %q", c.in, got, c.want)
		}
	}
	got := Text(`mysql --host db --password=s3cretPW9 -e "select 1"`).String()
	if strings.Contains(got, "s3cretPW9") {
		t.Errorf("secret survives: %q", got)
	}
	if !strings.Contains(got, `-e "select 1"`) {
		t.Errorf("the command was cut: %q", got)
	}
}

func TestLobeScrubSkKeys(t *testing.T) {
	for _, key := range []string{
		"sk-abcdefghijklmnopqrstuv",
		"sk-1234567890abcdefghijklmnopqr",
		"sk-ABCDEFGHIJKLMNOPQRSTUVWXYZabc",
		"sk-proj-abcdefghijklmnopqrstuvwxyz",
	} {
		got := Text("key " + key + " here").String()
		if !strings.Contains(got, "[redacted:api-key]") || strings.Contains(got, key) {
			t.Errorf("%s is not replaced: %q", key, got)
		}
	}
}

// TestLobeScrubURLLogin: the password of a URL login is replaced whatever the host, and only a
// stand-in value is left alone.
func TestLobeScrubURLLogin(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"postgres://app:Xk29pQ77vLm@localhost:5433/prod", "postgres://app:[redacted:url-login]@localhost:5433/prod"},
		{"mysql://root:root@127.0.0.1/dev", "mysql://root:[redacted:url-login]@127.0.0.1/dev"},
		{"postgres://app:$DB_PASSWORD@db.internal/main", "postgres://app:$DB_PASSWORD@db.internal/main"},
		{"https://user@example.test/path", "https://user@example.test/path"},
	} {
		if got := Text(c.in).String(); got != c.want {
			t.Errorf("Text(%q)\n got:  %q\n want: %q", c.in, got, c.want)
		}
	}
	got := Text("git clone https://ghp_abcdefghijklmnopqrstuvwxyz0123456789@github.com/o/r.git").String()
	if strings.Contains(got, "ghp_") || !strings.Contains(got, "@github.com/o/r.git") {
		t.Errorf("token in a clone link: %q", got)
	}
}

// TestLobeScrubLoginFlag: the password of a -p flag on a login, or after sshpass, is replaced; a
// placeholder, a short value, and a -p of any other command are left alone.
func TestLobeScrubLoginFlag(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"docker login -u bob -p Tr0ub4dor&3 registry.test", "docker login -u bob -p [redacted:flag] registry.test"},
		{`helm registry login -p "S3cret pass" reg.test`, `helm registry login -p "[redacted:flag]" reg.test`},
		{"sshpass -p hunter22 ssh user@host", "sshpass -p [redacted:flag] ssh user@host"},
		{"docker login -u bob -p $REGISTRY_PASSWORD registry.test", "docker login -u bob -p $REGISTRY_PASSWORD registry.test"},
		{"mkdir -p build/out", "mkdir -p build/out"},
		{"ssh -p 2222 user@host", "ssh -p 2222 user@host"},
		{"echo login && mkdir -p secretdir1", "echo login && mkdir -p secretdir1"},
	} {
		if got := Text(c.in).String(); got != c.want {
			t.Errorf("Text(%q)\n got:  %q\n want: %q", c.in, got, c.want)
		}
	}
}

// TestLobeScrubVendorShortPrefix: a key whose short prefix is part of a dashed candidate that is too
// short by itself is still caught (the candidate must not consume the start of the key).
func TestLobeScrubVendorShortPrefix(t *testing.T) {
	got := Text("ucpm-ui-civeocajsEhupeeb8ud02phagzaiaDy02.yrwFd2Apgi42ijF").String()
	if !strings.Contains(got, "[redacted:") {
		t.Errorf("not redacted: %q", got)
	}
	if strings.Contains(got, "civeocajsEhupeeb8ud02phagzaiaDy02") {
		t.Errorf("key survives: %q", got)
	}
}

// TestLobeScrubLine: a record of Clean fields and plain metadata becomes one JSON line, and the last
// check accepts it or refuses it with ErrUnsafe.
func TestLobeScrubLine(t *testing.T) {
	type rec struct {
		V     int    `json:"v"`
		ID    string `json:"id"`
		Tool  string `json:"tool"`
		Input Clean  `json:"input,omitzero"`
		Error Clean  `json:"error,omitzero"`
		Exit  *int   `json:"exit,omitempty"`
	}
	type decoded struct {
		Input string `json:"input"`
		Error string `json:"error"`
	}
	decode := func(s string) decoded {
		var d decoded
		if err := json.Unmarshal([]byte(s), &d); err != nil {
			t.Fatalf("not a record: %q (%v)", s, err)
		}
		return d
	}

	if !(Clean{}).IsZero() || Text("x").IsZero() {
		t.Errorf("IsZero: empty %v, text %v", Clean{}.IsZero(), Text("x").IsZero())
	}
	if m, err := Text("a && <b>").MarshalJSON(); err != nil || string(m) != `"a && <b>"` {
		t.Errorf("MarshalJSON: %q (%v)", m, err)
	}

	input := Input(json.RawMessage(`{"command":"a && b < c > d","password":"hunter2"}`))
	errText := Text("password=hunter2 rejected for postgres://app:s3cretPW@db.example.test/main")
	line, err := Line(rec{V: 1, ID: "r1", Tool: "Bash", Input: input, Error: errText})
	if err != nil {
		t.Fatalf("a clean record refused: %v", err)
	}
	s := line.String()
	if !json.Valid([]byte(s)) {
		t.Fatalf("not valid JSON: %q", s)
	}
	if want := `{"command":"a && b < c > d","password":"[redacted:key]"}`; decode(s).Input != want {
		t.Errorf("input: got %q want %q", decode(s).Input, want)
	}
	for _, mark := range []string{"&&", "<", ">"} {
		if !strings.Contains(s, mark) {
			t.Errorf("%q is not unescaped in %q", mark, s)
		}
	}
	for _, escaped := range []string{`\u0026`, `\u003c`, `\u003e`} {
		if strings.Contains(s, escaped) {
			t.Errorf("HTML-escaped %q in %q", escaped, s)
		}
	}
	for _, secret := range []string{"hunter2", "s3cretPW"} {
		if strings.Contains(s, secret) {
			t.Errorf("secret %q survives: %q", secret, s)
		}
	}

	zero := 0
	line, err = Line(rec{V: 2, ID: "r2", Tool: "Read", Input: input, Error: Text(""), Exit: &zero})
	if err != nil {
		t.Fatalf("refused: %v", err)
	}
	if s = line.String(); strings.Contains(s, `"error"`) || !strings.Contains(s, `"exit":0`) {
		t.Errorf("empty error or zero exit: %q", s)
	}
	line, err = Line(rec{V: 3, ID: "r3", Tool: "Read"})
	if err != nil {
		t.Fatalf("refused: %v", err)
	}
	if s = line.String(); strings.Contains(s, `"error"`) || strings.Contains(s, `"exit"`) {
		t.Errorf("an absent field is written: %q", s)
	}

	if _, err := Line(rec{V: 4, ID: "r4", Tool: "ghp_" + strings.Repeat("A", 36)}); err != ErrUnsafe {
		t.Errorf("a raw token in metadata: err %v, want ErrUnsafe", err)
	}

	const pathText = `open C:\Users\dev\project\src\auth\token_store.go: no such file`
	path := Text(pathText)
	line, err = Line(rec{V: 5, ID: "r5", Tool: "Read", Error: path})
	if err != nil {
		t.Fatalf("a Windows path refused: %v", err)
	}
	if got := decode(line.String()).Error; got != pathText {
		t.Errorf("path does not round-trip: got %q want %q", got, pathText)
	}

	e := strings.Repeat("x", 1000) + " ghp_" + strings.Repeat("A", 36) + " " + strings.Repeat("x", 5500)
	cut := Text(e)
	line, err = Line(rec{V: 6, ID: "r6", Tool: "Bash", Error: cut})
	if err != nil {
		t.Fatalf("a two-part text refused: %v", err)
	}
	if got := decode(line.String()).Error; got != cut.String() {
		t.Errorf("two-part text does not round-trip: %q", got)
	}
}

// TestLobeScrubCleanParity: every row the source files expect to stay as it is comes back unchanged.
func TestLobeScrubCleanParity(t *testing.T) {
	for _, c := range cleanCases {
		if got := Text(c.text).String(); got != c.text {
			t.Errorf("%s: changed\n in:  %q\n out: %q", c.desc, c.text, got)
		}
	}
}
