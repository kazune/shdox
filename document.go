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
	text         string
	eol          string
	omitted      bool
	insertBefore *sourceLine
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
	name        string
	summary     []string
	description [][]string
	tags        []tag
}

type analysis struct {
	lines     []sourceLine
	formatted []sourceLine
	fileDoc   *document
	functions []document
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
	commentLines := make(map[int]bool)
	closingBackquoteLines := make(map[int]bool)
	functions := make(map[int][]*syntax.Lit)
	syntax.Walk(tree, func(node syntax.Node) bool {
		switch node := node.(type) {
		case *syntax.Comment:
			line, column := position(node.Pos())
			text := result.lines[line-1].text
			if strings.Trim(text[:column-1], horizontal) == "" {
				commentLines[line] = true
				if strings.HasPrefix(text[column-1:], "##") {
					comments[line] = column
				}
			}
		case *syntax.CmdSubst:
			if node.Backquotes {
				line, _ := position(node.Right)
				closingBackquoteLines[line] = true
			}
		case *syntax.FuncDecl:
			line, column := position(node.Pos())
			if strings.Trim(result.lines[line-1].text[:column-1], horizontal) == "" {
				if node.Name != nil {
					functions[line] = []*syntax.Lit{node.Name}
				} else {
					functions[line] = node.Names
				}
			}
		case *syntax.TestDecl:
			line, column := position(node.Pos())
			if strings.Trim(result.lines[line-1].text[:column-1], horizontal) == "" {
				functions[line] = nil
			}
		}
		return true
	})
	// A closing backquote can end a shell comment before the physical line
	// ends. Comment.End derives its offset from the decoded comment text.
	for line := range closingBackquoteLines {
		delete(commentLines, line)
		delete(comments, line)
	}
	// splitSource keeps the CR in a CRLF ending separate from the line text.
	for i, line := range result.lines {
		if comments[i+1] == 0 {
			continue
		}
		if column := strings.IndexByte(line.text, '\r'); column >= 0 {
			return result, []diagnostic{{line: i + 1, column: column + 1, message: "carriage return must be followed by LF"}}
		}
	}

	headerEnd := 1
	for headerEnd <= len(result.lines) && (commentLines[headerEnd] ||
		strings.Trim(result.lines[headerEnd-1].text, horizontal) == "") {
		headerEnd++
	}
	for line := 1; line <= len(result.lines); {
		if comments[line] == 0 {
			line++
			continue
		}
		start := line
		for comments[line] != 0 {
			line++
		}
		names, declaration := functions[line]
		fileBlock := !declaration && start < headerEnd
		switch {
		case fileBlock && result.fileDoc != nil:
			diagnostics = append(diagnostics, diagnostic{
				line: start, column: comments[start], message: "file documentation must be a single contiguous block", violation: true,
			})
		case !fileBlock && len(names) == 0:
			diagnostics = append(diagnostics, diagnostic{
				line: start, column: comments[start], message: "documentation block must immediately precede a named function or be in the file header", violation: true,
			})
		}
		doc, issues := result.parseBlock(start, line, comments)
		diagnostics = append(diagnostics, issues...)
		if fileBlock && result.fileDoc == nil {
			result.fileDoc = &doc
		}
		for _, name := range names {
			functionDoc := doc
			functionDoc.name = name.Value
			result.functions = append(result.functions, functionDoc)
		}
	}
	if len(result.lines) != 0 && result.lines[len(result.lines)-1].eol == "" {
		for i := len(result.formatted) - 1; i >= 0; i-- {
			if !result.formatted[i].omitted {
				result.formatted[i].eol = ""
				break
			}
		}
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
	case "@env", "@global":
		t.argument, t.description = splitField(rest)
		if !syntax.ValidName(t.argument) {
			return t, fmt.Errorf("invalid variable name for %s: %q", name, t.argument)
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

func (a *analysis) parseBlock(start, end int, columns map[int]int) (document, []diagnostic) {
	var doc document
	var issues []diagnostic
	report := func(line int, message string) {
		issues = append(issues, diagnostic{line: line, column: columns[line], message: message, violation: true})
	}
	isBlank := func(line int) bool {
		return strings.Trim(a.lines[line-1].text[columns[line]+1:], horizontal) == ""
	}
	first := start
	for start < end && isBlank(start) {
		a.formatted[start-1].omitted = true
		start++
	}
	if start == end {
		report(first, "expected a summary")
		return doc, issues
	}
	for start < end && isBlank(end-1) {
		a.formatted[end-2].omitted = true
		end--
	}

	seen := make(map[string]bool)
	var tagLines []int
	blank, inTags := false, false
	for line := start; line < end; line++ {
		original := a.lines[line-1].text
		prefix := original[:columns[line]+1]
		suffix := strings.TrimRight(original[len(prefix):], horizontal)
		content := strings.TrimLeft(suffix, horizontal)
		switch {
		case suffix == "":
			a.formatted[line-1].text = prefix
			if inTags || blank {
				a.formatted[line-1].omitted = true
			}
			blank = true
			continue
		case suffix == content:
			report(line, "expected horizontal whitespace after ##")
		case strings.HasPrefix(content, "@"):
			if !inTags && len(doc.summary) == 0 {
				report(line, "expected a summary")
			} else if !inTags && !blank {
				a.formatted[line-1].insertBefore = &sourceLine{text: prefix, eol: a.lines[line-2].eol}
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
			tagLines = append(tagLines, line)
		default:
			a.formatted[line-1].text = prefix + " " + content
			switch {
			case inTags:
				report(line, "text is not allowed after tags")
			case !blank && len(doc.description) == 0:
				doc.summary = append(doc.summary, content)
			default:
				if blank {
					doc.description = append(doc.description, nil)
				}
				last := len(doc.description) - 1
				doc.description[last] = append(doc.description[last], content)
			}
		}
		blank = false
	}
	if len(issues) == 0 {
		index := 0
		for _, section := range orderedTagSections(doc.tags) {
			for _, item := range section.items {
				line := tagLines[index]
				prefix := a.lines[line-1].text[:columns[line]+1]
				a.formatted[line-1].text = prefix + " " + item.label() + " " + item.description
				index++
			}
		}
	}
	return doc, issues
}
