package scan

import (
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestPythonLinesStripsStringsAndComments(t *testing.T) {
	src := `import pandas as pd  # tq:allow ml/x: fixture
x = "a # not a comment"
df = pd.read_csv(
    "data/acme.csv",   # path
    sep=";",
)
def f():
    s = """multi
line"""
    return s
y = r"\d+" + 'it''s'
`
	ls := PythonLines([]byte(src))
	if len(ls) != 7 {
		for _, l := range ls {
			t.Logf("%d-%d indent=%d %q %q", l.Line, l.EndLine, l.Indent, l.Code, l.Comment)
		}
		t.Fatalf("got %d logical lines", len(ls))
	}
	if ls[0].Comment != "tq:allow ml/x: fixture" || ls[0].Code != "import pandas as pd" {
		t.Fatalf("line 1: %+v", ls[0])
	}
	if ls[1].Code != `x = ""` || ls[1].Strings[0] != "a # not a comment" {
		t.Fatalf("line 2: %+v", ls[1])
	}
	if ls[2].Line != 3 || ls[2].EndLine != 6 || !strings.Contains(ls[2].Code, `sep=""`) || ls[2].Strings[0] != "data/acme.csv" {
		t.Fatalf("joined call: %+v", ls[2])
	}
	if ls[3].Code != "def f():" || !ls[3].TopLevel() || ls[4].TopLevel() || ls[4].Indent != 4 {
		t.Fatalf("indent: %+v %+v", ls[3], ls[4])
	}
	if ls[4].Strings[0] != "multi\nline" || ls[5].Line != 10 {
		t.Fatalf("triple: %+v / %+v", ls[4], ls[5])
	}
	if ls[6].Strings[0] != `\d+` {
		t.Fatalf("raw: %+v", ls[6])
	}
}

func TestPythonCalls(t *testing.T) {
	src := `from sklearn.model_selection import train_test_split, GridSearchCV
import numpy as np
X_train, X_test = train_test_split(X, y, test_size=0.2, random_state=42)
scaler.fit(X)
if check(a == b):
    model = pipe.fit(X_train).predict(X_test)
`
	calls := PythonCalls([]byte(src))
	byName := map[string]Call{}
	for _, c := range calls {
		byName[c.Func] = c
	}
	tts := byName["train_test_split"]
	if tts.Line != 3 || !tts.TopLevel || len(tts.Args) != 2 || tts.Kwargs["random_state"] != "42" || tts.Kwargs["test_size"] != "0.2" {
		t.Fatalf("train_test_split: %+v", tts)
	}
	if c := byName["scaler.fit"]; c.Receiver != "scaler" || c.Name != "fit" {
		t.Fatalf("scaler.fit: %+v", c)
	}
	if c := byName["check"]; len(c.Kwargs) != 0 || len(c.Args) != 1 {
		t.Fatalf("== is not a kwarg: %+v", c)
	}
	if c := byName[".predict"]; c.Line != 6 || c.TopLevel {
		t.Fatalf("chained call: %+v", c)
	}
	if _, ok := byName["if"]; ok {
		t.Fatal("keyword taken for a call")
	}
	ims := PythonImports([]byte(src))
	if len(ims) != 2 || ims[0].Module != "sklearn.model_selection" || !reflect.DeepEqual(ims[0].Names, []string{"train_test_split", "GridSearchCV"}) || ims[1].Alias != "np" {
		t.Fatalf("imports: %+v", ims)
	}
}

func TestIPynbCells(t *testing.T) {
	nb := `{
 "cells": [
  {
   "cell_type": "markdown",
   "source": ["# Title\n"]
  },
  {
   "cell_type": "code",
   "source": ["import pandas as pd\n", "df = pd.read_csv('x.csv')"],
   "outputs": [{"output_type": "stream", "text": ["tester@acme.test\n"]}]
  }
 ],
 "nbformat": 4
}`
	cells, err := NotebookCells("a/train.ipynb", []byte(nb))
	if err != nil || len(cells) != 2 {
		t.Fatalf("%v %+v", err, cells)
	}
	if cells[1].Kind != "code" || cells[1].Line != 7 || cells[1].Outputs != 1 || !strings.Contains(cells[1].OutputText, "tester@acme.test") {
		t.Fatalf("code cell: %+v", cells[1])
	}
	src, lineOf := CodeSource(cells)
	calls := PythonCalls(src)
	if len(calls) != 1 || calls[0].Func != "pd.read_csv" || lineOf(calls[0].Line) != 7 {
		t.Fatalf("calls %+v line %d", calls, lineOf(calls[0].Line))
	}
}

func TestSynapseCells(t *testing.T) {
	nb := `{"name": "nb1", "properties": {"nbformat": 4, "metadata": {}, "cells": [
{"cell_type": "code", "metadata": {"microsoft": {"language": "sparksql"}}, "source": ["SELECT * FROM t"]},
{"cell_type": "code", "source": ["x = 1"], "outputs": []}]}}`
	cells, err := NotebookCells("notebook/nb1.json", []byte(nb))
	if err != nil || len(cells) != 2 || cells[0].Language != "sparksql" || cells[1].Source != "x = 1" {
		t.Fatalf("%v %+v", err, cells)
	}
	src, _ := CodeSource(cells)
	if strings.Contains(string(src), "SELECT") {
		t.Fatal("a sparksql cell is not python")
	}
}

func TestFabricCells(t *testing.T) {
	src := `# Fabric notebook source

# METADATA ********************

# META {
# META   "kernel_info": {"name": "synapse_pyspark"}
# META }

# MARKDOWN ********************

# # Load orders

# CELL ********************

df = spark.read.table("orders")
df.write.mode("overwrite").saveAsTable("acme")

# METADATA ********************

# META {
# META   "language": "python"
# META }

# PARAMETERS CELL ********************

run_date = "2026-01-01"
`
	cells := FabricCells([]byte(src))
	if len(cells) != 3 || cells[0].Kind != "markdown" || cells[0].Source != "# Load orders" {
		t.Fatalf("%+v", cells)
	}
	if cells[1].Kind != "code" || cells[1].Line != 15 || cells[1].Language != "python" || !strings.HasPrefix(cells[1].Source, "df = spark") {
		t.Fatalf("code: %+v", cells[1])
	}
	code, lineOf := CodeSource(cells)
	var save Call
	for _, c := range PythonCalls(code) {
		if c.Name == "saveAsTable" {
			save = c
		}
	}
	if lineOf(save.Line) != 16 {
		t.Fatalf("saveAsTable on file line %d, want 16", lineOf(save.Line))
	}
	if cells[2].Kind != "parameters" {
		t.Fatalf("params: %+v", cells[2])
	}
	if DetectNotebook("x/notebook-content.py", nil) != FormatFabric {
		t.Fatal("detect")
	}
}

func TestSQLTokensAndStatements(t *testing.T) {
	src := `-- header
select o.*, c.name, count(*) as n
from {{ ref('orders') }} o
join analytics."Customers" c on c.id = o.customer_id
where o.note = 'it''s; fine' /* ; */
group by 1, 2;
CREATE FUNCTION f() RETURNS int AS $body$ SELECT 1; $body$ LANGUAGE sql;
select * from a, b.c as x, [dbo].[t];
`
	stmts := SQLStatements([]byte(src))
	if len(stmts) != 3 {
		t.Fatalf("%d statements", len(stmts))
	}
	s0 := stmts[0]
	if s0.Keyword() != "SELECT" || s0.Line != 2 || s0.EndLine != 6 {
		t.Fatalf("s0: %s %d-%d", s0.Keyword(), s0.Line, s0.EndLine)
	}
	rels := s0.Relations()
	if len(rels) != 2 || !rels[0].Jinja || !strings.Contains(rels[0].Name, "ref('orders')") || rels[1].Name != "analytics.Customers" || rels[1].Line != 4 {
		t.Fatalf("relations: %+v", rels)
	}
	if got := s0.SelectStars(); !reflect.DeepEqual(got, []int{2}) {
		t.Fatalf("o.* is a star: %v", got)
	}
	if stmts[1].Keyword() != "CREATE" {
		t.Fatalf("dollar body split: %s", stmts[1].Keyword())
	}
	rels = stmts[2].Relations()
	var names []string
	for _, r := range rels {
		names = append(names, r.Name)
	}
	if !reflect.DeepEqual(names, []string{"a", "b.c", "dbo.t"}) {
		t.Fatalf("from list: %v", names)
	}
	if got := stmts[2].SelectStars(); len(got) != 1 || got[0] != 8 {
		t.Fatalf("stars: %v", got)
	}
}

func TestSQLFromInsideFunctionIsNotARelation(t *testing.T) {
	st := SQLStatements([]byte("select extract(year from created_at) from orders"))
	rels := st[0].Relations()
	if len(rels) != 1 || rels[0].Name != "orders" {
		t.Fatalf("%+v", rels)
	}
}

func TestYAMLHelpers(t *testing.T) {
	n, err := ParseYAML([]byte("models:\n  - name: churn\n    paths: [a, b]\nanchor: &x {k: 1}\nref: *x\n"))
	if err != nil {
		t.Fatal(err)
	}
	m := Get(n, "models", "0")
	if Scalar(Get(m, "name")) != "churn" || Line(Get(m, "name")) != 2 || !reflect.DeepEqual(Strings(Get(m, "paths")), []string{"a", "b"}) {
		t.Fatalf("get: %+v", m)
	}
	if Scalar(Get(n, "ref", "k")) != "1" {
		t.Fatal("alias")
	}
	if es := Entries(n); len(es) != 3 || es[0].Key != "models" || es[2].Line != 5 {
		t.Fatalf("entries: %+v", es)
	}
	var paths []string
	Walk(n, func(p []string, _ *yaml.Node) bool { paths = append(paths, strings.Join(p, ".")); return true })
	if !contains(paths, "models.[0].paths.[1]") {
		t.Fatalf("walk: %v", paths)
	}
	docs, err := ParseYAMLAll([]byte("a: 1\n---\nb: 2\n"))
	if err != nil || len(docs) != 2 {
		t.Fatalf("%v %d", err, len(docs))
	}
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
