package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"mvdan.cc/sh/v3/syntax"
)

func invoke(input string, args ...string) (int, string, string) {
	var stdout, stderr strings.Builder
	status := run(args, strings.NewReader(input), &stdout, &stderr)
	return status, stdout.String(), stderr.String()
}

func TestDocumentGrammar(t *testing.T) {
	tests := []struct {
		name, block, issue string
	}{
		{"summary", "## Summary", ""},
		{"details", "## Summary\n##\n## One\n## Two\n##\n## Three", ""},
		{"tags", "## Summary\n##\n## @return 0 OK\n## @param $10 Path\n## @exit 0 Done\n## @return 255 Fail\n## @param $1 Name\n## @stdout A\n## @sideeffect B\n## @sideeffect B", ""},
		{"missing summary", "##\n## Details", "summary"},
		{"tag as summary", "## @stdout A", "summary"},
		{"prefix", "### Summary", "whitespace"},
		{"missing detail separator", "## Summary\n## Details", "blank line"},
		{"missing tag separator", "## Summary\n## @stdout A", "blank line"},
		{"consecutive blanks", "## Summary\n##\n##\n## Details", "consecutive"},
		{"trailing blank", "## Summary\n## \t", "cannot end"},
		{"blank between tags", "## Summary\n##\n## @stdout A\n##\n## @stderr B", "between tags"},
		{"text after tags", "## Summary\n##\n## @stdout A\n## Text", "after tags"},
		{"zero argument", "## Summary\n##\n## @param $0 Zero", "invalid position"},
		{"leading zero argument", "## Summary\n##\n## @param $01 One", "invalid position"},
		{"negative status", "## Summary\n##\n## @return -1 Negative", "invalid status"},
		{"leading zero status", "## Summary\n##\n## @return 00 Zero", "invalid status"},
		{"out of range status", "## Summary\n##\n## @exit 256 High", "invalid status"},
		{"missing description", "## Summary\n##\n## @exit 2 \t", "missing description"},
		{"unknown tag", "## Summary\n##\n## \t@unknown A", "unknown tag"},
		{"duplicate param", "## Summary\n##\n## @param $1 A\n## @param $1 B", "duplicate tag"},
		{"duplicate status", "## Summary\n##\n## @return 1 A\n## @return 1 B", "duplicate tag"},
		{"duplicate stream", "## Summary\n##\n## @stdin A\n## @stdin B", "duplicate tag"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			status, out, diagnostics := invoke(test.block+"\nf() { :; }\n", "check", "-")
			if test.issue == "" {
				if status != 0 || out != "" || diagnostics != "" {
					t.Fatalf("check = %d, %q, %q", status, out, diagnostics)
				}
			} else if status != 2 || out != "" || !strings.Contains(diagnostics, test.issue) {
				t.Fatalf("check = %d, %q, %q; expected %q", status, out, diagnostics, test.issue)
			}
		})
	}
}

func TestDetectionAndFormatting(t *testing.T) {
	before := "echo x ## inline  \r\n" +
		"cat <<'EOF'\r\n## untouched\r\nEOF\r\n" +
		"echo 'first\n## quoted\nlast'\n" +
		"if true; then\n\t##\t Summary  text \r\n\t## \t\n\t## \tDetails\tinside\t\n" +
		"\t##\r\n\t##\t@param \t$1\t *File*  \r\n\tfunction f { :; }\r\nfi\n" +
		"outer() {\n ## Nested\n function inner() { :; }\n}"
	want := strings.NewReplacer(
		"##\t Summary  text ", "## Summary  text",
		"## \t\n", "##\n",
		"## \tDetails\tinside\t", "## Details\tinside",
		"##\t@param \t$1\t *File*  ", "## @param $1 *File*",
	).Replace(before)

	for _, input := range []string{before, before, want} {
		status, out, diagnostics := invoke(input, "fmt", "-")
		if status != 0 || out != want || diagnostics != "" {
			t.Fatalf("fmt = %d, %q, %q; want %q", status, out, diagnostics, want)
		}
	}
	original, _ := analyze("-", before, syntax.LangAuto)
	formatted, _ := analyze("-", want, syntax.LangAuto)
	if len(original.documents) != 2 || !reflect.DeepEqual(original.documents, formatted.documents) {
		t.Fatalf("parsed structure changed: %#v → %#v", original.documents, formatted.documents)
	}
	for _, source := range []string{
		"## Orphan",
		"## Summary\n\nf() { :; }\n",
		"## Summary\n# ordinary\nf() { :; }\n",
		": # inline\n\n## Orphan",
	} {
		if status, _, diagnostics := invoke(source, "check", "-"); status != 2 || !strings.Contains(diagnostics, "immediately precede") {
			t.Fatalf("check %q = %d, %q", source, status, diagnostics)
		}
	}
}

func TestGenerate(t *testing.T) {
	first := filepath.Join(t.TempDir(), "first.sh")
	writeTestFile(t, first, "## *First*\r\nf() { :; }\r\n")
	input := "## Second\n##\n## [Link](./target)\n##\n## @return 255 **Failure**\nf() { :; }"
	want := "## `f`\n\n*First*\n\n## `f`\n\nSecond\n\n[Link](./target)\n\n- `@return 255` **Failure**\n"
	status, out, diagnostics := invoke(input, "generate", first, "-")
	if status != 0 || out != want || diagnostics != "" {
		t.Fatalf("generate = %d, %q, %q", status, out, diagnostics)
	}
	status, out, diagnostics = invoke("f() { :; }", "generate", "-")
	if status != 0 || out != "" || diagnostics != "" {
		t.Fatalf("empty generate = %d, %q, %q", status, out, diagnostics)
	}
}

func TestDiff(t *testing.T) {
	input := ":\n:\n:\n:\n##\tSummary\nf() { :; }\n:\n:"
	want := "--- -\n+++ -\n@@ -2,7 +2,7 @@\n :\n :\n :\n-##\tSummary\n+## Summary\n f() { :; }\n :\n :\n\\ No newline at end of file\n"
	status, out, diagnostics := invoke(input, "fmt", "-d", "-")
	if status != 1 || out != want || diagnostics != "" {
		t.Fatalf("fmt -d = %d, %q, %q", status, out, diagnostics)
	}
	status, out, diagnostics = invoke(strings.Replace(input, "\t", " ", 1), "fmt", "-d", "-")
	if status != 0 || out != "" || diagnostics != "" {
		t.Fatalf("clean fmt -d = %d, %q, %q", status, out, diagnostics)
	}
}

func TestErrorsAndPreflight(t *testing.T) {
	dir := t.TempDir()
	first, broken := filepath.Join(dir, "first.sh"), filepath.Join(dir, "broken.sh")
	invalidDoc := filepath.Join(dir, "invalid-doc.sh")
	invalidSource := "\t### Bad\n##\n## @unknown Err\nf() { :; }\n"
	writeTestFile(t, invalidDoc, invalidSource)
	original := "##\tFirst\nf() { :; }\n"
	writeTestFile(t, first, original)
	writeTestFile(t, broken, "if\n")
	for _, args := range [][]string{
		{"check"}, {"fmt"}, {"fmt", "-d"}, {"fmt", "-w"}, {"generate"},
	} {
		docName := "-"
		if len(args) == 2 && args[1] == "-w" {
			docName = invalidDoc
		}
		args = append(args, first, broken, docName)
		status, out, diagnostics := invoke(invalidSource, args...)
		bash := strings.Index(diagnostics, broken+":")
		line1 := strings.Index(diagnostics, docName+":1:2:")
		line3 := strings.Index(diagnostics, docName+":3:1:")
		if status != 2 || out != "" || !(bash >= 0 && bash < line1 && line1 < line3) {
			t.Fatalf("%v = %d, %q, %q", args, status, out, diagnostics)
		}
	}
	if data, err := os.ReadFile(first); err != nil || string(data) != original {
		t.Fatalf("preflight changed first input: %q, %v", data, err)
	}

	for _, args := range [][]string{
		{"fmt", "-w", "-"}, {"fmt", "-d", "-w", first}, {"check", "-", "-"}, {"check"},
	} {
		status, out, diagnostics := invoke("", args...)
		if status != 2 || out != "" || !strings.HasPrefix(diagnostics, "shdox: ") {
			t.Fatalf("%v = %d, %q, %q", args, status, out, diagnostics)
		}
	}
	for _, test := range []struct{ source, prefix string }{
		{"\uFEFF:", "-:1:1:"}, {"# あ\xff", "-:1:6:"},
	} {
		status, out, diagnostics := invoke(test.source, "check", "-")
		if status != 2 || out != "" || !strings.HasPrefix(diagnostics, test.prefix) {
			t.Fatalf("check %q = %d, %q, %q", test.source, status, out, diagnostics)
		}
	}
	if status, _, diagnostics := invoke("", "check", dir); status != 2 || !strings.HasPrefix(diagnostics, dir+":") {
		t.Fatalf("directory input = %d, %q", status, diagnostics)
	}
	status, out, diagnostics := invoke("", "fmt", "-w", first)
	if data, err := os.ReadFile(first); status != 0 || out != "" || diagnostics != "" || err != nil || string(data) != "## First\nf() { :; }\n" {
		t.Fatalf("fmt -w = %d, %q, %q; file = %q, %v", status, out, diagnostics, data, err)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func TestOutputFailure(t *testing.T) {
	var diagnostics strings.Builder
	if status := run([]string{"fmt", "-d", "-"}, strings.NewReader("##\tSummary\nf() { :; }\n"), failingWriter{}, &diagnostics); status != 2 || !strings.Contains(diagnostics.String(), "write failed") {
		t.Fatalf("output failure = %d, %q", status, &diagnostics)
	}
}

func TestLongIndent(t *testing.T) {
	input := strings.Repeat(" ", 20_000) + "##\tSummary\nf() { :; }\n"
	status, out, diagnostics := invoke(input, "fmt", "-")
	if status != 0 || out != strings.Replace(input, "\t", " ", 1) || diagnostics != "" {
		t.Fatalf("long indent failed: status %d, diagnostics %q", status, diagnostics)
	}
}

func writeTestFile(t *testing.T, name, source string) {
	t.Helper()
	if err := os.WriteFile(name, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestMultipleInputs(t *testing.T) {
	dir := t.TempDir()
	first, second := filepath.Join(dir, "first.sh"), filepath.Join(dir, "second.sh")
	input := "##\tSummary\nf() { :; }"
	writeTestFile(t, first, input)
	writeTestFile(t, second, strings.Replace(input, "Summary", "Second", 1))
	status, out, diagnostics := invoke(input, "fmt", first, "-", second)
	want := "## Summary\nf() { :; }## Summary\nf() { :; }## Second\nf() { :; }"
	if status != 0 || out != want || diagnostics != "" {
		t.Fatalf("fmt = %d, %q, %q", status, out, diagnostics)
	}
	status, out, diagnostics = invoke(input, "fmt", "-d", first, "-", second)
	a := strings.Index(out, "--- "+first+"\n")
	b := strings.Index(out, "--- -\n")
	c := strings.Index(out, "--- "+second+"\n")
	if status != 1 || diagnostics != "" || !(a == 0 && a < b && b < c) {
		t.Fatalf("fmt -d = %d, %q, %q", status, out, diagnostics)
	}
}

func TestWriteStopsAfterFailure(t *testing.T) {
	dir := t.TempDir()
	first, blocked, last := filepath.Join(dir, "first.sh"), filepath.Join(dir, "blocked.sh"), filepath.Join(dir, "last.sh")
	original := "##\tSummary\nf() { :; }\n"
	for _, name := range []string{first, blocked, last} {
		writeTestFile(t, name, original)
	}
	if err := os.Chmod(blocked, 0o400); err != nil {
		t.Fatal(err)
	}
	if file, err := os.OpenFile(blocked, os.O_WRONLY, 0); err == nil {
		file.Close()
		t.Skip("test environment permits writing to read-only files")
	}
	status, out, diagnostics := invoke("", "fmt", "-w", first, blocked, last)
	if status != 2 || out != "" || !strings.HasPrefix(diagnostics, blocked+":") {
		t.Fatalf("fmt -w = %d, %q, %q", status, out, diagnostics)
	}
	for _, item := range []struct{ name, want string }{
		{first, strings.Replace(original, "\t", " ", 1)}, {blocked, original}, {last, original},
	} {
		data, err := os.ReadFile(item.name)
		if err != nil || string(data) != item.want {
			t.Fatalf("%s = %q, %v; want %q", item.name, data, err, item.want)
		}
	}
}

func TestDetectLanguage(t *testing.T) {
	tests := []struct {
		name, source string
		want         syntax.LangVariant
	}{
		{"script.bash", "#!/bin/sh\n", syntax.LangPOSIX},
		{"script.zsh", "#!/usr/bin/env bash\r\n", syntax.LangBash},
		{"script.mksh", "#!/bin/unknown\n", syntax.LangMirBSDKorn},
		{"script.bats", "", syntax.LangBats},
		{"script.zsh", "", syntax.LangZsh},
		{"script.bash", "", syntax.LangBash},
		{"script.sh", "", syntax.LangBash},
		{"-", "#!/usr/bin/env dash", syntax.LangPOSIX},
		{"-", "", syntax.LangBash},
	}
	for _, test := range tests {
		if got := detectLanguage(test.name, test.source); got != test.want {
			t.Errorf("detectLanguage(%q, %q) = %s, want %s", test.name, test.source, got, test.want)
		}
	}
}

func TestLanguageSelectionAcrossInputs(t *testing.T) {
	dir := t.TempDir()
	first, second := filepath.Join(dir, "first.bash"), filepath.Join(dir, "second.sh")
	posix := "## Portable\nf() { :; }\n"
	bash := "## Bash\nf() { values=(one two); }\n"
	writeTestFile(t, first, bash)
	writeTestFile(t, second, "#!/bin/sh\n"+posix)
	for _, command := range []string{"check", "fmt", "generate"} {
		status, _, diagnostics := invoke("#!/usr/bin/env bash\n"+bash, command, first, second, "-")
		if status != 0 || diagnostics != "" {
			t.Fatalf("%s auto = %d, %q", command, status, diagnostics)
		}
		status, out, diagnostics := invoke("#!/usr/bin/env bash\n"+bash, command, "--lang=posix", first, second, "-")
		if status != 2 || out != "" || !strings.Contains(diagnostics, first+":") ||
			!strings.Contains(diagnostics, "-:3:") || strings.Contains(diagnostics, second+":") {
			t.Fatalf("%s --lang=posix = %d, %q, %q", command, status, out, diagnostics)
		}
	}
	writeTestFile(t, second, "#!/bin/sh\n"+bash)
	if status, _, diagnostics := invoke("#!/bin/sh\n"+bash, "check", first, second, "-"); status != 2 ||
		!strings.Contains(diagnostics, second+":") || !strings.Contains(diagnostics, "-:3:") {
		t.Fatalf("auto must use POSIX for sh shebangs: %d, %q", status, diagnostics)
	}
	if status, _, diagnostics := invoke("#!/bin/sh\n"+bash, "check", first, "--lang", "bash", second, "-"); status != 0 || diagnostics != "" {
		t.Fatalf("explicit Bash must override both shebangs: %d, %q", status, diagnostics)
	}
	for _, args := range [][]string{
		{"check", "--lang"},
		{"fmt", "--lang", "-"},
		{"generate", "--lang=", "-"},
		{"check", "--lang=fish", first, "-"},
	} {
		status, out, diagnostics := invoke("", args...)
		if status != 2 || out != "" || !strings.HasPrefix(diagnostics, "shdox: ") {
			t.Fatalf("%v = %d, %q, %q", args, status, out, diagnostics)
		}
	}
}

func TestLanguageSpecificDeclarations(t *testing.T) {
	source := "## Shared\nfunction alpha beta { :; }\n"
	want := "## `alpha`\n\nShared\n\n## `beta`\n\nShared\n"
	status, out, diagnostics := invoke(source, "generate", "--lang=zsh", "-")
	if status != 0 || out != want || diagnostics != "" {
		t.Fatalf("Zsh multi-name generation = %d, %q, %q", status, out, diagnostics)
	}
	for _, test := range []struct{ language, declaration string }{
		{"zsh", "() { :; }\n"},
		{"bats", "@test \"works\" { :; }\n"},
	} {
		status, _, diagnostics := invoke(test.declaration, "check", "--lang", test.language, "-")
		if status != 0 || diagnostics != "" {
			t.Fatalf("%s input = %d, %q", test.language, status, diagnostics)
		}
		status, _, diagnostics = invoke("## Misplaced\n"+test.declaration, "check", "--lang", test.language, "-")
		if status != 2 || !strings.Contains(diagnostics, "immediately precede") {
			t.Fatalf("documented %s declaration = %d, %q", test.language, status, diagnostics)
		}
	}
}
