package main

import (
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"mvdan.cc/sh/v3/fileutil"
	"mvdan.cc/sh/v3/syntax"
)

const horizontal = " \t"

type sourceLine struct {
	text    string
	eol     string
	omitted bool
}

func splitSource(source string) []sourceLine {
	var lines []sourceLine
	for source != "" {
		text, rest, found := strings.Cut(source, "\n")
		line := sourceLine{text: text}
		if found {
			line.eol = "\n"
			if strings.HasSuffix(text, "\r") {
				line.text = text[:len(text)-1]
				line.eol = "\r\n"
			}
		}
		lines = append(lines, line)
		source = rest
	}
	return lines
}

type diagnostic struct {
	line, column int
	message      string
	violation    bool
}

type tag struct {
	name, argument, description string
}

func (t tag) label() string {
	if t.argument != "" {
		return t.name + " " + t.argument
	}
	return t.name
}

type document struct {
	name    string
	summary string
	details [][]string
	tags    []tag
}

type analysis struct {
	lines     []sourceLine
	formatted []sourceLine
	documents []document
}

func detectLanguage(name, source string) syntax.LangVariant {
	firstLine, _, _ := strings.Cut(source, "\n")
	interpreter := fileutil.Shebang([]byte(firstLine))
	if interpreter == "" {
		switch extension := filepath.Ext(name); extension {
		case ".bash", ".mksh", ".bats", ".zsh":
			interpreter = extension[1:]
		}
	}
	var variant syntax.LangVariant
	if variant.Set(interpreter) == nil {
		return variant
	}
	return syntax.LangBash
}

func analyze(name, source string, language syntax.LangVariant) (analysis, []diagnostic) {
	var result analysis
	if strings.HasPrefix(source, "\uFEFF") {
		return result, []diagnostic{{line: 1, column: 1, message: "UTF-8 BOM is not allowed"}}
	}
	if !utf8.ValidString(source) {
		line, column := 1, 1
		for len(source) > 0 {
			r, size := utf8.DecodeRuneInString(source)
			if r == utf8.RuneError && size == 1 {
				return result, []diagnostic{{line: line, column: column, message: "invalid UTF-8"}}
			}
			if r == '\n' {
				line, column = line+1, 1
			} else {
				column += size
			}
			source = source[size:]
		}
	}

	// Normalize only the parser input so its line and column positions can
	// address the original lines, including those terminated by CRLF.
	if language == syntax.LangAuto {
		language = detectLanguage(name, source)
	}
	parser := syntax.NewParser(syntax.Variant(language), syntax.KeepComments(true))
	normalized := strings.ReplaceAll(source, "\r\n", "\n")
	tree, err := parser.Parse(strings.NewReader(normalized), name)
	if err != nil {
		// The parser's error already includes the input name and position.
		return result, []diagnostic{{message: err.Error()}}
	}
	result.lines = splitSource(source)
	starts := make([]int, len(result.lines))
	offset := 0
	for i, line := range result.lines {
		starts[i] = offset
		offset += len(line.text) + len(line.eol)
		if line.eol == "\r\n" {
			offset--
		}
	}
	position := func(pos syntax.Pos) (line, column int) {
		offset := int(pos.Offset())
		line = sort.Search(len(starts), func(i int) bool { return starts[i] > offset })
		return line, offset - starts[line-1] + 1
	}
	result.formatted = slices.Clone(result.lines)
	var diagnostics []diagnostic
	comments := make(map[int]int)
	functions := make(map[int][]*syntax.Lit)
	syntax.Walk(tree, func(node syntax.Node) bool {
		switch node := node.(type) {
		case *syntax.Comment:
			line, column := position(node.Pos())
			text := result.lines[line-1].text
			if strings.HasPrefix(text[column-1:], "##") && strings.Trim(text[:column-1], horizontal) == "" {
				comments[line] = column
			}
		case *syntax.FuncDecl:
			line, column := position(node.Pos())
			if strings.Trim(result.lines[line-1].text[:column-1], horizontal) == "" {
				if node.Name != nil {
					functions[line] = []*syntax.Lit{node.Name}
				} else if len(node.Names) != 0 {
					functions[line] = node.Names
				}
			}
		}
		return true
	})

	for line := 1; line <= len(result.lines); {
		if comments[line] == 0 {
			line++
			continue
		}
		start := line
		for comments[line] != 0 {
			line++
		}
		names := functions[line]
		doc, issues := result.parseBlock(start, line, len(names) != 0, comments)
		for _, name := range names {
			doc.name = name.Value
			result.documents = append(result.documents, doc)
		}
		diagnostics = append(diagnostics, issues...)
	}
	return result, diagnostics
}

func parseTag(content string) (tag, error) {
	name, rest := splitField(content)
	t := tag{name: name, description: rest}
	switch name {
	case "@param", "@return", "@exit":
		t.argument, t.description = splitField(rest)
		if name == "@param" {
			position, found := strings.CutPrefix(t.argument, "$")
			if !found || position == "0" || !decimal(position) {
				return t, fmt.Errorf("invalid position for %s: %q", name, t.argument)
			}
		} else {
			value, err := strconv.Atoi(t.argument)
			if !decimal(t.argument) || err != nil || value > 255 {
				return t, fmt.Errorf("invalid status for %s: %q", name, t.argument)
			}
		}
	case "@stdin", "@stdout", "@stderr", "@sideeffect":
	default:
		return t, fmt.Errorf("unknown tag %q", name)
	}
	if t.description == "" {
		return t, fmt.Errorf("missing description for %s", name)
	}
	return t, nil
}

func splitField(text string) (string, string) {
	if i := strings.IndexAny(text, horizontal); i >= 0 {
		return text[:i], strings.TrimLeft(text[i:], horizontal)
	}
	return text, ""
}

func decimal(text string) bool {
	if text == "" || (len(text) > 1 && text[0] == '0') {
		return false
	}
	for i := range len(text) {
		if text[i] < '0' || text[i] > '9' {
			return false
		}
	}
	return true
}

func (a *analysis) parseBlock(start, end int, hasFunction bool, columns map[int]int) (document, []diagnostic) {
	var doc document
	var issues []diagnostic
	report := func(line int, message string) {
		issues = append(issues, diagnostic{line: line, column: columns[line], message: message, violation: true})
	}
	if !hasFunction {
		report(start, "documentation block must immediately precede a function declaration")
	}

	seen := make(map[string]bool)
	blank, inTags := false, false
	for line := start; line < end; line++ {
		original := a.lines[line-1].text
		prefix := original[:columns[line]+1]
		suffix := strings.TrimRight(original[len(prefix):], horizontal)
		content := strings.TrimLeft(suffix, horizontal)
		switch {
		case suffix == "":
			a.formatted[line-1].text = prefix
			switch {
			case line == start:
				report(line, "expected a summary")
			case line == end-1:
				report(line, "documentation block cannot end with a blank line")
			case inTags:
				report(line, "blank line between tags")
			case blank:
				a.formatted[line-1].omitted = true
			}
			blank = true
			continue
		case suffix == content:
			report(line, "expected horizontal whitespace after ##")
		case strings.HasPrefix(content, "@"):
			if line == start {
				report(line, "expected a summary")
			} else if !inTags && !blank {
				report(line, "expected a blank line before tags")
			}
			inTags = true
			t, err := parseTag(content)
			if err != nil {
				report(line, err.Error())
				break
			}
			if t.name != "@sideeffect" && seen[t.label()] {
				report(line, "duplicate tag "+t.label())
			}
			seen[t.label()] = true
			doc.tags = append(doc.tags, t)
		default:
			a.formatted[line-1].text = prefix + " " + content
			switch {
			case line == start:
				doc.summary = content
			case inTags:
				report(line, "text is not allowed after tags")
			default:
				if len(doc.details) == 0 {
					if !blank {
						report(line, "expected a blank line after the summary")
					}
					doc.details = append(doc.details, nil)
				} else if blank {
					doc.details = append(doc.details, nil)
				}
				last := len(doc.details) - 1
				doc.details[last] = append(doc.details[last], content)
			}
		}
		blank = false
	}
	if len(issues) == 0 {
		// A valid block ends with all of its tag lines, with no gaps.
		line := end - len(doc.tags)
		for _, section := range orderedTagSections(doc.tags) {
			for _, item := range section.items {
				prefix := a.lines[line-1].text[:columns[line]+1]
				a.formatted[line-1].text = prefix + " " + item.label() + " " + item.description
				line++
			}
		}
	}
	return doc, issues
}
