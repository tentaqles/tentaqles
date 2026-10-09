package decide

import "testing"

func TestIsVerificationCommand(t *testing.T) {
	count := []string{
		"go test ./...",
		"go vet ./internal/...",
		"cd app && npm test",
		"cd app; npm run lint",
		"VAR=1 go test ./...",
		"CGO_ENABLED=0 GOOS=linux go build ./cmd/tq",
		"env CI=1 npm test",
		"npm run lint",                    // PowerShell or bash
		"Set-Location app; npm run build", // PowerShell
		"npm run test:unit",
		"pnpm lint",
		"yarn typecheck",
		"bun test",
		"npx tsc --noEmit",
		"npx jest src",
		"pytest -q tests",
		"python -m pytest tests",
		"python3 -m unittest discover",
		"uv run pytest",
		"poetry run mypy .",
		"cargo test --all",
		"cargo clippy -- -D warnings",
		"make test",
		"tsc -p .",
		"eslint src",
		"ruff check .",
		"dotnet test",
		"dotnet build MySln.sln",
		"mvn test",
		"./gradlew test",
		"gradle check",
		"Invoke-Pester",
		"go test ./... 2>&1 | tail -20",
		"time go test ./...",
		"C:/tools/node/npm.cmd test",
		"git status && go test ./...",
	}
	notCount := []string{
		`echo "npm test passed"`,
		`printf 'pytest ok'`,
		"cat notes.md # go test",
		`grep -r "make test" .`,
		`rg "go test" docs`,
		`Write-Host "npm test passed"`,
		`Write-Output 'go test ok'`,
		`echo go test`,
		`echo "a; pytest"`,
		`echo 'x' && "pytest"`,
		"# go test ./...",
		"npm test || true",
		"go test ./... 2>&1 || echo failed",
		"cat <<EOF > notes.md\ngo test ./...\nEOF",
		"npm install",
		"npm run dev",
		"go run ./cmd/tq",
		"make",
		"python script.py pytest",
		"git commit -m 'go test passes'",
		`echo "unterminated`,
		"ls tests/",
		"sed -n 1,10p pytest.ini",
	}
	for _, c := range count {
		if !IsVerificationCommand(c) {
			t.Errorf("must count: %q", c)
		}
	}
	for _, c := range notCount {
		if IsVerificationCommand(c) {
			t.Errorf("must not count: %q", c)
		}
	}
}
