package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime/debug"
	"strings"
	"testing"

	"mvdan.cc/sh/v3/syntax"
)

func invoke(input string, args ...string) (int, string, string) {
	var stdout, stderr strings.Builder
	status := run(args, strings.NewReader(input), &stdout, &stderr)
	return status, stdout.String(), stderr.String()
}

func checkFormatInvariants(t *testing.T, source, name string, options ...string) (formatted, markdown string) {
	t.Helper()
	input, path := source, name
	if name != "-" {
		path = filepath.Join(t.TempDir(), filepath.Base(name))
		writeTestFile(t, path, source)
		input = ""
	}
	args := append([]string{""}, options...)
	args = append(args, "--", path)
	invokeSuccess := func(command, phase string) string {
		t.Helper()
		args[0] = command
		status, out, diagnostics := invoke(input, args...)
		if status != 0 || diagnostics != "" {
			t.Fatalf("%s %s formatting %q = %d, %q, %q", command, phase, name, status, out, diagnostics)
		}
		return out
	}
	if out := invokeSuccess("check", "before"); out != "" {
		t.Fatalf("check before formatting %q wrote %q", name, out)
	}
	markdown = invokeSuccess("generate", "before")
	formatted = invokeSuccess("fmt", "before")
	if name == "-" {
		input = formatted
	} else {
		writeTestFile(t, path, formatted)
	}
	if out := invokeSuccess("check", "after"); out != "" {
		t.Errorf("check after formatting %q wrote %q", name, out)
	}
	if out := invokeSuccess("generate", "after"); out != markdown {
		t.Errorf("generate after formatting %q = %q; before = %q", name, out, markdown)
	}
	if out := invokeSuccess("fmt", "after"); out != formatted {
		t.Errorf("fmt after formatting %q = %q; first pass = %q", name, out, formatted)
	}
	return formatted, markdown
}

func writeValidTestFile(t *testing.T, name, source string, options ...string) {
	t.Helper()
	checkFormatInvariants(t, source, name, options...)
	writeTestFile(t, name, source)
}

func TestDocumentGrammar(t *testing.T) {
	tests := []struct {
		name, block, issue string
	}{
		{"summary", "## Summary", ""},
		{"details", "## Summary\n##\n## One\n## Two\n##\n## Three", ""},
		{"tags", "## Summary\n##\n## @return 0 OK\n## @param $10 Path\n## @exit 0 Done\n## @return 255 Fail\n## @param $1 Name\n## @stdout A\n## @sideeffect B\n## @sideeffect B", ""},
		{"leading blank", "##\n## Summary", ""},
		{"blank-only block", "##\n## \t", "summary"},
		{"tag before summary after blank", "##\n## @stdout A", "summary"},
		{"tag as summary", "## @stdout A", "summary"},
		{"prefix", "### Summary", "whitespace"},
		{"multiline summary", "## Summary\n## Continuation", ""},
		{"optional tag separator", "## Summary\n## @stdout A", ""},
		{"consecutive blanks", "## Summary\n##\n##\n## Details", ""},
		{"consecutive blanks between tags", "## Summary\n##\n## @stdin A\n##\n##\n## @stderr B", ""},
		{"consecutive trailing blanks", "## Summary\n##\n##", ""},
		{"trailing blank", "## Summary\n## \t", ""},
		{"blank between tags", "## Summary\n##\n## @stdout A\n##\n## @stderr B", ""},
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
			source := test.block + "\nf() { :; }\n"
			if test.issue == "" {
				checkFormatInvariants(t, source, "-")
				return
			}
			if status, out, diagnostics := invoke(source, "check", "-"); status != 1 || out != "" || !strings.Contains(diagnostics, test.issue) {
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

	checkFormatInvariants(t, before, "-")
	for _, input := range []string{before, before, want} {
		status, out, diagnostics := invoke(input, "fmt", "-")
		if status != 0 || out != want || diagnostics != "" {
			t.Fatalf("fmt = %d, %q, %q; want %q", status, out, diagnostics, want)
		}
	}
	original, _ := analyze("-", before, syntax.LangAuto)
	formatted, _ := analyze("-", want, syntax.LangAuto)
	if len(original.functions) != 2 || !reflect.DeepEqual(original.functions, formatted.functions) {
		t.Fatalf("parsed structure changed: %#v → %#v", original.functions, formatted.functions)
	}
	for _, source := range []string{
		":\n## Orphan",
		":\n## Summary\n\nf() { :; }\n",
		":\n## Summary\n# ordinary\nf() { :; }\n",
		": # inline\n\n## Orphan",
	} {
		if status, _, diagnostics := invoke(source, "check", "-"); status != 1 || !strings.Contains(diagnostics, "immediately precede") {
			t.Fatalf("check %q = %d, %q", source, status, diagnostics)
		}
	}
}

func TestCommentEndsAtClosingBackquote(t *testing.T) {
	const source = "x=`\n## Summary` printf \"%s\\n\" \\ \nf() { :; }\n"
	formatted, markdown := checkFormatInvariants(t, source, "-")
	if formatted != source || markdown != "" {
		t.Errorf("fmt = %q, generate = %q; want fmt output %q and empty Markdown", formatted, markdown, source)
	}

	const separated = "x=`\n## Orphan\n## ignored`\nf() { :; }\n"
	status, _, diagnostics := invoke(separated, "check", "-")
	if status != 1 || !strings.HasPrefix(diagnostics, "-:2:1: documentation block must immediately precede") {
		t.Errorf("check = %d, %q; want placement diagnostic for line 2", status, diagnostics)
	}
}

func TestFormatWholeLineComments(t *testing.T) {
	for _, test := range []struct{ source, want string }{
		{"\t##\t概要 \\\r\nf() { :; }\n", "\t## 概要 \\\r\nf() { :; }\n"},
		{"##\tEOF", "## EOF"},
		{"x=`\n ##\tEscaped \\`tick\\`\n f() { :; }\n`\n", "x=`\n ## Escaped \\`tick\\`\n f() { :; }\n`\n"},
	} {
		out, _ := checkFormatInvariants(t, test.source, "-")
		if out != test.want {
			t.Errorf("fmt %q = %q; want %q", test.source, out, test.want)
		}
	}
}

func TestGenerate(t *testing.T) {
	first := filepath.Join(t.TempDir(), "first.sh")
	writeValidTestFile(t, first, "## *First*\r\nf() { :; }\r\n")
	input := "## Second\n## Continuation\n##\n## [Link](./target)\n##\n## @return 255 **Failure**\nf() { :; }"
	checkFormatInvariants(t, input, "-")
	want := "# " + codeSpan(first) + "\n\n## Functions\n\n### `f`\n\n*First*\n\n# `-`\n\n## Functions\n\n### `f`\n\nSecond\nContinuation\n\n#### Description\n\n[Link](./target)\n\n#### Return\n\n- `255`: **Failure**\n"
	status, out, diagnostics := invoke(input, "generate", first, "-")
	if status != 0 || out != want || diagnostics != "" {
		t.Fatalf("generate = %d, %q, %q", status, out, diagnostics)
	}
	if _, out := checkFormatInvariants(t, "f() { :; }", "-"); out != "" {
		t.Fatalf("empty generate = %q", out)
	}
}

func TestDiff(t *testing.T) {
	input := ":\n:\n:\n:\n##\tSummary\nf() { :; }\n:\n:"
	checkFormatInvariants(t, input, "-")
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

func TestDiffQuotesInputNames(t *testing.T) {
	const source = "##\tSummary\n"
	checkFormatInvariants(t, source, "-")
	before := splitSource(source)
	after := splitSource("## Summary\n")
	const hunk = "@@ -1 +1 @@\n-##\tSummary\n+## Summary\n"
	for _, test := range []struct{ name, label string }{
		{"script.sh", "script.sh"},
		{"名前.sh", "名前.sh"},
		{"-", "-"},
		{"two words.sh", `"two words.sh"`},
		{"part\nnext.sh", `"part\nnext.sh"`},
		{"part\t\rnext.sh", `"part\t\rnext.sh"`},
		{`quote"and\slash.sh`, `"quote\"and\\slash.sh"`},
		{"controls\a\b\f\v\x01\x7f.sh", `"controls\a\b\f\v\001\177.sh"`},
		{"invalid\xff.sh", `"invalid\377.sh"`},
		{"separator\u2028.sh", `"separator\342\200\250.sh"`},
	} {
		var out strings.Builder
		want := "--- " + test.label + "\n+++ " + test.label + "\n" + hunk
		if !writeDiff(&out, test.name, before, after) || out.String() != want {
			t.Errorf("diff for %q = %q; want %q", test.name, out.String(), want)
		}
	}
}

func TestVersion(t *testing.T) {
	const source = ":\n"
	stdin := strings.NewReader(source)
	var stdout, stderr strings.Builder
	status := run([]string{"--version"}, stdin, &stdout, &stderr)
	info, _ := debug.ReadBuildInfo()
	if want := "shdox " + buildVersion(info) + "\n"; status != 0 || stdout.String() != want || stderr.Len() != 0 || stdin.Len() != len(source) {
		t.Fatalf("--version = %d, %q, %q with %d unread bytes; want %q and unread input",
			status, stdout.String(), stderr.String(), stdin.Len(), want)
	}
	if status := run([]string{"--version"}, stdin, failingWriter{}, &stderr); status != 2 ||
		!strings.Contains(stderr.String(), "writing standard output: write failed") || stdin.Len() != len(source) {
		t.Errorf("--version with failing output = %d, %q with %d unread bytes", status, stderr.String(), stdin.Len())
	}
}

func TestBuildVersion(t *testing.T) {
	if got := buildVersion(nil); got != devVersion {
		t.Errorf("no build info gives %q; want %q", got, devVersion)
	}
	for _, test := range []struct {
		version, want string
		settings      []debug.BuildSetting
	}{
		{version: "v1.2.3", want: "1.2.3"},
		{version: "v1.2.3-rc.1", want: "1.2.3-rc.1"},
		{version: "v2.0.0+incompatible", want: "2.0.0"},
		{version: "", want: devVersion},
		{version: "(devel)", want: devVersion},
		{version: "v0.0.0-20261127000000-0123456789ab", want: devVersion},
		{version: "v1.2.4-0.20261127000000-0123456789ab", want: devVersion},
		{version: "v1.2.3-rc.1.0.20261127000000-0123456789ab", want: devVersion},
		{version: "v1.2.3+dirty", want: devVersion},
		{version: "v2.0.0+incompatible.dirty", want: devVersion},
		{version: "v1.2.3", settings: []debug.BuildSetting{{Key: "vcs.modified", Value: "true"}}, want: devVersion},
	} {
		info := &debug.BuildInfo{Main: debug.Module{Version: test.version}, Settings: test.settings}
		if got := buildVersion(info); got != test.want {
			t.Errorf("version %q with settings %v = %q; want %q", test.version, test.settings, got, test.want)
		}
	}
}

func TestUsageOnArgumentErrors(t *testing.T) {
	for _, test := range []struct {
		args    []string
		message string
	}{
		{nil, "expected --version, check, fmt, or generate"},
		{[]string{"--version", "-"}, "--version does not take arguments"},
		{[]string{"check", "--version", "-"}, `unknown option "--version" for check`},
		{[]string{"unknown", "-"}, `unknown command "unknown"`},
		{[]string{"check", "-d", "-"}, `unknown option "-d" for check`},
		{[]string{"check"}, "expected at least one input file"},
		{[]string{"check", "-", "-"}, "standard input can only be specified once"},
		{[]string{"fmt", "-d", "-w", "script.sh"}, "-d and -w cannot be combined"},
		{[]string{"fmt", "-w", "-"}, "-w cannot be used with standard input"},
		{[]string{"generate", "--lang", "-"}, "--lang requires a value"},
		{[]string{"generate", "--lang=fish", "-"}, `unknown language "fish"`},
	} {
		const source = ":\n"
		stdin := strings.NewReader(source)
		var stdout, stderr strings.Builder
		status := run(test.args, stdin, &stdout, &stderr)
		want := "shdox: " + test.message + "\n\n" + usage
		if status != 2 || stdout.Len() != 0 || stderr.String() != want || stdin.Len() != len(source) {
			t.Errorf("%v = %d, %q, %q, %d unread bytes; want diagnostic %q and unread input",
				test.args, status, stdout.String(), stderr.String(), stdin.Len(), want)
		}
	}
	status, out, diagnostics := invoke("if\n", "check", "-")
	if status != 2 || out != "" || !strings.HasPrefix(diagnostics, "-:1:1: ") || strings.Contains(diagnostics, "Usage:") {
		t.Errorf("shell syntax error = %d, %q, %q", status, out, diagnostics)
	}
}

func TestErrorsAndPreflight(t *testing.T) {
	dir := t.TempDir()
	first, broken := filepath.Join(dir, "first.sh"), filepath.Join(dir, "broken.sh")
	invalidDoc := filepath.Join(dir, "invalid-doc.sh")
	invalidSource := "\t### Bad\n##\n## @unknown Err\nf() { :; }\n"
	writeTestFile(t, invalidDoc, invalidSource)
	original := "##\tFirst\nf() { :; }\n"
	writeValidTestFile(t, first, original)
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

func TestRejectBareCarriageReturnInDocumentation(t *testing.T) {
	const message = "carriage return must be followed by LF\n"
	for _, test := range []struct{ source, position string }{
		{"## Summary\n## @stdout Output\n## @stdin Input\r", "-:3:16: "},
		{"# Header\r\n:\n## あ\rvalue\r", "-:3:7: "},
		{"## Summary\r\r\n", "-:1:11: "},
	} {
		for _, args := range [][]string{{"check", "-"}, {"fmt", "-"}, {"fmt", "-d", "-"}, {"generate", "-"}} {
			status, out, diagnostics := invoke(test.source, args...)
			if status != 2 || out != "" || diagnostics != test.position+message {
				t.Errorf("%v with %q = %d, %q, %q", args, test.source, status, out, diagnostics)
			}
		}
	}

	dir := t.TempDir()
	first, invalid, last := filepath.Join(dir, "first.sh"), filepath.Join(dir, "invalid.sh"), filepath.Join(dir, "last.sh")
	const original = "#!/bin/bash\r\n##\tSummary\nf() { :; }"
	writeValidTestFile(t, first, original)
	writeTestFile(t, invalid, "## File\r")
	writeTestFile(t, last, ":\n## Orphan\n")
	status, out, diagnostics := invoke("", "fmt", "-w", first, invalid, last)
	if status != 2 || out != "" || !strings.HasPrefix(diagnostics, invalid+":1:8: "+message+last+":2:1: ") {
		t.Errorf("fmt -w = %d, %q, %q", status, out, diagnostics)
	}
	if data, err := os.ReadFile(first); err != nil || string(data) != original {
		t.Errorf("preflight changed first input: %q, %v", data, err)
	}
	out, _ = checkFormatInvariants(t, original, "-")
	if want := strings.Replace(original, "\t", " ", 1); out != want {
		t.Errorf("fmt with mixed line endings = %q; want %q", out, want)
	}
}

func TestPreserveBareCarriageReturnOutsideDocumentation(t *testing.T) {
	const source = "# Ordinary\r comment\r\n" +
		": ## Inline\r comment\n" +
		"printf '%s\\n' 'quoted\r text'\n" +
		"cat <<'EOF'\n## Here\r doc\nEOF\n" +
		"x=`\n## Ignored\r comment`\n" +
		"##\tSummary\nf() { :; }\n# Tail\r"
	formatted, markdown := checkFormatInvariants(t, source, "-")
	if want := strings.Replace(source, "##\tSummary", "## Summary", 1); formatted != want {
		t.Errorf("fmt = %q; want %q", formatted, want)
	}
	if want := "# `-`\n\n## Functions\n\n### `f`\n\nSummary\n"; markdown != want {
		t.Errorf("generate = %q; want %q", markdown, want)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func TestOutputFailure(t *testing.T) {
	const source = "##\tSummary\nf() { :; }\n"
	checkFormatInvariants(t, source, "-")
	var diagnostics strings.Builder
	if status := run([]string{"fmt", "-d", "-"}, strings.NewReader(source), failingWriter{}, &diagnostics); status != 2 || !strings.Contains(diagnostics.String(), "write failed") {
		t.Fatalf("output failure = %d, %q", status, &diagnostics)
	}
}

func TestLongIndent(t *testing.T) {
	input := strings.Repeat(" ", 20_000) + "##\tSummary\nf() { :; }\n"
	out, _ := checkFormatInvariants(t, input, "-")
	if out != strings.Replace(input, "\t", " ", 1) {
		t.Fatal("fmt did not preserve the long indent")
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
	checkFormatInvariants(t, input, "-")
	writeValidTestFile(t, first, input)
	writeValidTestFile(t, second, strings.Replace(input, "Summary", "Second", 1))
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
		writeValidTestFile(t, name, original)
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
		checkFormatInvariants(t, test.source, test.name)
	}
}

func TestLanguageSelectionAcrossInputs(t *testing.T) {
	dir := t.TempDir()
	first, second := filepath.Join(dir, "first.bash"), filepath.Join(dir, "second.sh")
	posix := "## Portable\nf() { :; }\n"
	bash := "## Bash\nf() { values=(one two); }\n"
	const bashOverride = "--lang=bash"
	stdin := "#!/usr/bin/env bash\n" + bash
	checkFormatInvariants(t, stdin, "-")
	checkFormatInvariants(t, bash, first, bashOverride)
	checkFormatInvariants(t, "#!/bin/sh\n"+posix, second, "--lang=posix")
	writeValidTestFile(t, first, bash)
	writeValidTestFile(t, second, "#!/bin/sh\n"+posix)
	for _, command := range []string{"check", "fmt", "generate"} {
		status, _, diagnostics := invoke(stdin, command, first, second, "-")
		if status != 0 || diagnostics != "" {
			t.Fatalf("%s auto = %d, %q", command, status, diagnostics)
		}
		status, out, diagnostics := invoke(stdin, command, "--lang=posix", first, second, "-")
		if status != 2 || out != "" || !strings.Contains(diagnostics, first+":") ||
			!strings.Contains(diagnostics, "-:3:") || strings.Contains(diagnostics, second+":") {
			t.Fatalf("%s --lang=posix = %d, %q, %q", command, status, out, diagnostics)
		}
	}
	stdin = "#!/bin/sh\n" + bash
	writeValidTestFile(t, second, stdin, bashOverride)
	checkFormatInvariants(t, stdin, "-", bashOverride)
	if status, _, diagnostics := invoke(stdin, "check", first, second, "-"); status != 2 ||
		!strings.Contains(diagnostics, second+":") || !strings.Contains(diagnostics, "-:3:") {
		t.Fatalf("auto must use POSIX for sh shebangs: %d, %q", status, diagnostics)
	}
	if status, _, diagnostics := invoke(stdin, "check", first, "--lang", "bash", second, "-"); status != 0 || diagnostics != "" {
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
	want := "# `-`\n\n## Functions\n\n### `alpha`\n\nShared\n\n### `beta`\n\nShared\n"
	if _, out := checkFormatInvariants(t, source, "-", "--lang=zsh"); out != want {
		t.Fatalf("Zsh multi-name generation = %q; want %q", out, want)
	}
	for _, test := range []struct{ language, declaration string }{
		{"zsh", "() { :; }\n"},
		{"bats", "@test \"works\" { :; }\n"},
	} {
		checkFormatInvariants(t, test.declaration, "-", "--lang", test.language)
		status, _, diagnostics := invoke("## Misplaced\n"+test.declaration, "check", "--lang", test.language, "-")
		if status != 1 || !strings.Contains(diagnostics, "immediately precede") {
			t.Fatalf("documented %s declaration = %d, %q", test.language, status, diagnostics)
		}
	}
}

func TestGenerateTagSections(t *testing.T) {
	const source = `## Summary
##
## @sideeffect Writes [logs](./logs)
## @exit 10 Fatal
## @return 255 Error
## @param $1000000000000000000000000000000 Large
## @stderr *Diagnostics*
## @param $10 Tenth
## @stdout **Output**
## @param $2 Second
## @stdin Input
## @return 2 Difference
## @exit 1 Missing
## @sideeffect Updates cache
## @return 0 OK
check_file() { :; }
`
	const want = "# `-`\n\n## Functions\n\n### `check_file`\n\nSummary\n\n" +
		"#### Param\n\n- `$2`: Second\n- `$10`: Tenth\n- `$1000000000000000000000000000000`: Large\n\n" +
		"#### Stdin\n\nInput\n\n" +
		"#### Stdout\n\n**Output**\n\n" +
		"#### Stderr\n\n*Diagnostics*\n\n" +
		"#### Return\n\n- `0`: OK\n- `2`: Difference\n- `255`: Error\n\n" +
		"#### Exit\n\n- `1`: Missing\n- `10`: Fatal\n\n" +
		"#### Side Effect\n\n- Writes [logs](./logs)\n- Updates cache\n"
	const formatted = `## Summary
##
## @param $2 Second
## @param $10 Tenth
## @param $1000000000000000000000000000000 Large
## @stdin Input
## @stdout **Output**
## @stderr *Diagnostics*
## @return 0 OK
## @return 2 Difference
## @return 255 Error
## @exit 1 Missing
## @exit 10 Fatal
## @sideeffect Writes [logs](./logs)
## @sideeffect Updates cache
check_file() { :; }
`
	actualSource, markdown := checkFormatInvariants(t, source, "-")
	if markdown != want {
		t.Errorf("generate = %q; want %q", markdown, want)
	}
	if actualSource != formatted {
		t.Errorf("fmt = %q; want %q", actualSource, formatted)
	}
}

func TestFormatTagSortPreservesLineLayout(t *testing.T) {
	input := "## Summary\n##\r\n\t## @return\t10 Ten  \r\n" +
		"  ## @param $1 File\n ## @return 2 Two\r\nf() { :; }"
	want := "## Summary\n##\r\n\t## @param $1 File\r\n" +
		"  ## @return 2 Two\n ## @return 10 Ten\r\nf() { :; }"
	if out, _ := checkFormatInvariants(t, input, "-"); out != want {
		t.Fatalf("fmt = %q; want %q", out, want)
	}
	input = strings.Replace(input, "@return\t10 Ten  ", "@return 10 Ten", 1)
	checkFormatInvariants(t, input, "-")
	status, out, diagnostics := invoke(input, "fmt", "-d", "-")
	if status != 1 || !strings.Contains(out, "+\t## @param $1 File\r\n") || diagnostics != "" {
		t.Fatalf("fmt -d for reorder only = %d, %q, %q", status, out, diagnostics)
	}
}

func TestCheckStatusPrecedence(t *testing.T) {
	const violation = ":\n## Orphan\n"
	for _, command := range []string{"fmt", "generate"} {
		status, out, diagnostics := invoke(violation, command, "-")
		if status != 2 || out != "" || !strings.Contains(diagnostics, "immediately precede") {
			t.Fatalf("%s = %d, %q, %q", command, status, out, diagnostics)
		}
	}

	file := filepath.Join(t.TempDir(), "invalid.sh")
	for _, bad := range []string{"if\n", "# invalid UTF-8: \xff\n"} {
		writeTestFile(t, file, bad)
		for _, files := range [][]string{{"-", file}, {file, "-"}} {
			status, out, diagnostics := invoke(violation, append([]string{"check"}, files...)...)
			if status != 2 || out != "" || !strings.Contains(diagnostics, file+":") ||
				!strings.Contains(diagnostics, "immediately precede") {
				t.Fatalf("check %v = %d, %q, %q", files, status, out, diagnostics)
			}
		}
	}
}

func TestFormatRepeatedBlankLines(t *testing.T) {
	input := "## Summary\n## Continuation\n\t## \t\r\n  ##\n## First\n##\n##\n## Second\n##\n##\n## @return 0 OK\nf() { :; }"
	want := "## Summary\n## Continuation\n\t##\r\n## First\n##\n## Second\n##\n## @return 0 OK\nf() { :; }"
	formatted, markdown := checkFormatInvariants(t, input, "-")
	if formatted != want {
		t.Errorf("fmt = %q; want %q", formatted, want)
	}
	const wantMarkdown = "# `-`\n\n## Functions\n\n### `f`\n\nSummary\nContinuation\n\n#### Description\n\nFirst\n\nSecond\n\n#### Return\n\n- `0`: OK\n"
	if markdown != wantMarkdown {
		t.Errorf("generate = %q; want %q", markdown, wantMarkdown)
	}
}

func TestDiffRepeatedBlankLineDeletions(t *testing.T) {
	first := "## Summary\n##\n##\n## Details\nf() { :; }\n"
	second := "## Summary\n##\n##\n## @return 0 OK\ng() { :; }"
	input := first + strings.Repeat(":\n", 8) + second
	checkFormatInvariants(t, input, "-")
	want := "--- -\n+++ -\n" +
		"@@ -1,6 +1,5 @@\n ## Summary\n ##\n-##\n ## Details\n f() { :; }\n :\n" +
		"@@ -13,6 +12,5 @@\n :\n ## Summary\n ##\n-##\n ## @return 0 OK\n g() { :; }\n\\ No newline at end of file\n"
	status, out, diagnostics := invoke(input, "fmt", "-d", "-")
	if status != 1 || out != want || diagnostics != "" {
		t.Fatalf("fmt -d = %d, %q, %q; want %q", status, out, diagnostics, want)
	}
}

func TestFormatBlockEdgeBlankLines(t *testing.T) {
	input := "\t## \t\r\n##\n## Summary\n##\n## @return 1 Different\n## @return 0 OK\n## \t\r\n  ##\nf() { :; }"
	want := "## Summary\n##\n## @return 0 OK\n## @return 1 Different\nf() { :; }"
	formatted, markdown := checkFormatInvariants(t, input, "-")
	if formatted != want {
		t.Errorf("fmt = %q; want %q", formatted, want)
	}
	const wantMarkdown = "# `-`\n\n## Functions\n\n### `f`\n\nSummary\n\n#### Return\n\n- `0`: OK\n- `1`: Different\n"
	if markdown != wantMarkdown {
		t.Errorf("generate = %q; want %q", markdown, wantMarkdown)
	}
	status, out, diagnostics := invoke(input, "fmt", "-d", "-")
	if status != 1 || diagnostics != "" || !strings.Contains(out, "@@ -1,9 +1,5 @@\n") ||
		!strings.Contains(out, "-\t## \t\r\n") || !strings.Contains(out, "-  ##\n") {
		t.Fatalf("fmt -d = %d, %q, %q", status, out, diagnostics)
	}
	for _, invalid := range []string{"##\n## \t\nf() { :; }\n", "##\n## @return 0 OK\n##\nf() { :; }\n"} {
		for _, command := range []string{"fmt", "generate"} {
			status, out, diagnostics := invoke(invalid, command, "-")
			if status != 2 || out != "" || !strings.Contains(diagnostics, "expected a summary") {
				t.Fatalf("%s = %d, %q, %q", command, status, out, diagnostics)
			}
		}
	}
}

func TestFileDocumentation(t *testing.T) {
	const source = "#!/bin/sh\n\n# License\n## Script summary\n##\n## Script description\n##\n" +
		"## @exit 1 Failure\n## @return 0 OK\n## @param $1 File\n\n" +
		"## Validate\nf() { :; }\n\n## Main\n##\n## @exit 0 OK\nmain() { :; }\n"
	const want = "# `-`\n\nScript summary\n\n## Description\n\nScript description\n\n" +
		"## Param\n\n- `$1`: File\n\n## Return\n\n- `0`: OK\n\n## Exit\n\n- `1`: Failure\n\n" +
		"## Functions\n\n### `f`\n\nValidate\n\n### `main`\n\nMain\n\n#### Exit\n\n- `0`: OK\n"
	formatted, markdown := checkFormatInvariants(t, source, "-")
	if formatted == source {
		t.Error("fmt did not change the source")
	}
	if markdown != want {
		t.Errorf("generate = %q; want %q", markdown, want)
	}
}

func TestFileDocumentationPlacement(t *testing.T) {
	for _, separator := range []string{"\n", "# ordinary comment\n"} {
		source := "## First\n" + separator + "## Second\n:\n"
		status, out, diagnostics := invoke(source, "check", "-")
		if status != 1 || out != "" || !strings.Contains(diagnostics, "-:3:1: file documentation must be a single contiguous block") {
			t.Fatalf("split header with %q: %d, %q, %q", separator, status, out, diagnostics)
		}
	}
	const fileOnly = "##\r\n## Script\r\n##"
	formatted, markdown := checkFormatInvariants(t, fileOnly, "-")
	if formatted != "## Script" {
		t.Errorf("fmt file only = %q", formatted)
	}
	if markdown != "# `-`\n\nScript\n" {
		t.Errorf("generate file only = %q", markdown)
	}
}

func TestGenerateSkipsUndocumentedInputs(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "undocumented.sh")
	documented := filepath.Join(dir, "documented.sh")
	writeValidTestFile(t, empty, ":\n")
	writeValidTestFile(t, documented, "## File\n:\n")
	const stdin = "## Function\nf() { :; }\n"
	checkFormatInvariants(t, stdin, "-")
	status, out, diagnostics := invoke(stdin, "generate", empty, documented, "-", empty)
	want := "# " + codeSpan(documented) + "\n\nFile\n\n# `-`\n\n## Functions\n\n### `f`\n\nFunction\n"
	if status != 0 || out != want || diagnostics != "" {
		t.Fatalf("generate = %d, %q, %q; want %q", status, out, diagnostics, want)
	}
}

func TestFormatTagBlankLines(t *testing.T) {
	tests := []struct {
		name, input, want string
	}{
		{
			"summary before tags",
			"## Summary\r\n\t## @return 2 Error\n##\r\n##\n ## @return 0 OK\r\nf() { :; }",
			"## Summary\r\n\t##\r\n\t## @return 0 OK\n ## @return 2 Error\r\nf() { :; }",
		},
		{
			"description before tags",
			"## Summary\n##\n## Description\r\n  ## @stdin Input\n##\n## @stdout Output\nf() { :; }",
			"## Summary\n##\n## Description\r\n  ##\r\n  ## @stdin Input\n## @stdout Output\nf() { :; }",
		},
		{
			"multiple before tags",
			"## Summary\n  ##\r\n##\n## @stdin Input\n##\n## @stdout Output\nf() { :; }",
			"## Summary\n  ##\r\n## @stdin Input\n## @stdout Output\nf() { :; }",
		},
		{
			"file documentation with no final newline",
			"## Summary\n## @stdin Input",
			"## Summary\n##\n## @stdin Input",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			formatted, markdown := checkFormatInvariants(t, test.input, "-")
			if formatted != test.want {
				t.Errorf("fmt = %q; want %q", formatted, test.want)
			}
			if markdown == "" {
				t.Error("generate returned empty Markdown")
			}
		})
	}
}

func TestDiffTagBlankLineInsertion(t *testing.T) {
	input := ":\n:\n:\n## Summary\n## @stdin Input\nf() { :; }\n:\n:\n"
	checkFormatInvariants(t, input, "-")
	want := "--- -\n+++ -\n@@ -2,6 +2,7 @@\n :\n :\n ## Summary\n+##\n ## @stdin Input\n f() { :; }\n :\n"
	status, out, diagnostics := invoke(input, "fmt", "-d", "-")
	if status != 1 || out != want || diagnostics != "" {
		t.Fatalf("fmt -d = %d, %q, %q; want %q", status, out, diagnostics, want)
	}
}
