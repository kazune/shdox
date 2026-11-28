package main

import (
	"cmp"
	"fmt"
	"io"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

func writeSource(out io.StringWriter, lines []sourceLine) {
	for _, line := range lines {
		if inserted := line.insertBefore; inserted != nil {
			out.WriteString(inserted.text)
			out.WriteString(inserted.eol)
		}
		if line.omitted {
			continue
		}
		out.WriteString(line.text)
		out.WriteString(line.eol)
	}
}

func writeMarkdown(out io.Writer, name string, fileDoc *document, functions []document, firstFunctionID int) {
	fmt.Fprintf(out, "# %s\n", codeSpan(name))
	if fileDoc != nil {
		writeDocument(out, *fileDoc, "##")
	}
	if len(functions) != 0 {
		fmt.Fprintln(out, "\n## Function Summary\n\n| Function | Summary |\n| --- | --- |")
		for i, doc := range functions {
			fmt.Fprintf(out, "| [%s](#shdox-function-%d) | %s |\n", codeSpan(doc.name), firstFunctionID+i, strings.Join(doc.summary, " "))
		}
		fmt.Fprintln(out, "\n## Function Details")
		for i, doc := range functions {
			fmt.Fprintf(out, "\n<a id=\"shdox-function-%d\"></a>\n\n### %s\n", firstFunctionID+i, codeSpan(doc.name))
			writeDocument(out, doc, "####")
		}
	}
}

func writeDocument(out io.Writer, doc document, heading string) {
	fmt.Fprintf(out, "\n%s\n", strings.Join(doc.summary, "\n"))
	if len(doc.description) != 0 {
		fmt.Fprintf(out, "\n%s Description\n", heading)
		for _, paragraph := range doc.description {
			fmt.Fprintf(out, "\n%s\n", strings.Join(paragraph, "\n"))
		}
	}
	for _, section := range orderedTagSections(doc.tags) {
		fmt.Fprintf(out, "\n%s %s\n\n", heading, section.heading)
		for _, item := range section.items {
			switch {
			case item.argument != "":
				fmt.Fprintf(out, "- %s: %s\n", codeSpan(item.argument), item.description)
			case item.name == "@sideeffect":
				fmt.Fprintf(out, "- %s\n", item.description)
			default:
				fmt.Fprintln(out, item.description)
			}
		}
	}
}

type tagSection struct {
	heading string
	items   []tag
}

func orderedTagSections(tags []tag) []tagSection {
	groups := make(map[string][]tag)
	for _, item := range tags {
		groups[item.name] = append(groups[item.name], item)
	}
	order := [...]struct{ name, heading string }{
		{"@param", "Param"},
		{"@stdin", "Stdin"},
		{"@stdout", "Stdout"},
		{"@stderr", "Stderr"},
		{"@return", "Return"},
		{"@exit", "Exit"},
		{"@sideeffect", "Side Effect"},
	}
	var sections []tagSection
	for _, section := range order {
		items := groups[section.name]
		if len(items) == 0 {
			continue
		}
		if items[0].argument != "" {
			// Arguments use canonical decimal digits and a common prefix within
			// each section, so length followed by lexical order is numeric order.
			slices.SortFunc(items, func(a, b tag) int {
				return cmp.Or(cmp.Compare(len(a.argument), len(b.argument)), cmp.Compare(a.argument, b.argument))
			})
		}
		sections = append(sections, tagSection{section.heading, items})
	}
	return sections
}

func codeSpan(text string) string {
	width, run := 1, 0
	for _, r := range text {
		if r == '`' {
			run++
			width = max(width, run+1)
		} else {
			run = 0
		}
	}
	fence := strings.Repeat("`", width)
	if strings.HasPrefix(text, "`") || strings.HasSuffix(text, "`") ||
		(strings.HasPrefix(text, " ") && strings.HasSuffix(text, " ") && strings.Trim(text, " ") != "") {
		return fence + " " + text + " " + fence
	}
	return fence + text + fence
}

type diffLine struct {
	prefix byte
	line   sourceLine
}

// Formatted lines stay aligned with the input, with insertions attached to
// the following input line and deletions marked omitted.
func diffLines(before, after []sourceLine) []diffLine {
	var lines, added []diffLine
	for i, line := range after {
		if line.insertBefore != nil {
			added = append(added, diffLine{'+', *line.insertBefore})
			line.insertBefore = nil
		}
		if before[i] == line {
			lines = append(lines, added...)
			added = added[:0]
			lines = append(lines, diffLine{' ', line})
			continue
		}
		lines = append(lines, diffLine{'-', before[i]})
		if !line.omitted {
			added = append(added, diffLine{'+', line})
		}
	}
	return append(lines, added...)
}

func writeDiff(out io.Writer, name string, before, after []sourceLine) bool {
	if slices.Equal(before, after) {
		return false
	}
	const contextLines = 3
	lines := diffLines(before, after)
	label := quoteDiffName(name)
	fmt.Fprintf(out, "--- %s\n+++ %s\n", label, label)

	oldOffset, newOffset, processed := 0, 0, 0
	for first := 0; first < len(lines); {
		if lines[first].prefix == ' ' {
			first++
			continue
		}
		start := max(processed, first-contextLines)
		oldOffset += start - processed
		newOffset += start - processed

		last := first
		for i := first + 1; i < len(lines) && i-last <= 2*contextLines+1; i++ {
			if lines[i].prefix != ' ' {
				last = i
			}
		}
		end := min(len(lines), last+contextLines+1)
		oldCount, newCount := 0, 0
		for _, edit := range lines[start:end] {
			if edit.prefix != '+' {
				oldCount++
			}
			if edit.prefix != '-' {
				newCount++
			}
		}
		fmt.Fprintf(out, "@@ -%s +%s @@\n", diffRange(oldOffset, oldCount), diffRange(newOffset, newCount))
		for _, edit := range lines[start:end] {
			writeDiffLine(out, edit.prefix, edit.line)
		}
		oldOffset += oldCount
		newOffset += newCount
		processed = end
		first = end
	}
	return true
}

func quoteDiffName(name string) string {
	needsQuotes := func(r rune) bool {
		return r == ' ' || r == '"' || r == '\\' || !unicode.IsPrint(r)
	}
	if utf8.ValidString(name) && strings.IndexFunc(name, needsQuotes) == -1 {
		return name
	}

	var quoted strings.Builder
	quoted.WriteByte('"')
	for name != "" {
		r, size := utf8.DecodeRuneInString(name)
		text := name[:size]
		name = name[size:]
		switch r {
		case '"', '\\':
			quoted.WriteByte('\\')
			quoted.WriteString(text)
		case '\a':
			quoted.WriteString(`\a`)
		case '\b':
			quoted.WriteString(`\b`)
		case '\f':
			quoted.WriteString(`\f`)
		case '\n':
			quoted.WriteString(`\n`)
		case '\r':
			quoted.WriteString(`\r`)
		case '\t':
			quoted.WriteString(`\t`)
		case '\v':
			quoted.WriteString(`\v`)
		default:
			if unicode.IsPrint(r) && !(r == utf8.RuneError && size == 1) {
				quoted.WriteString(text)
				continue
			}
			for i := range len(text) {
				fmt.Fprintf(&quoted, `\%03o`, text[i])
			}
		}
	}
	quoted.WriteByte('"')
	return quoted.String()
}

func diffRange(start, count int) string {
	switch count {
	case 0:
		return fmt.Sprintf("%d,0", start)
	case 1:
		return fmt.Sprintf("%d", start+1)
	default:
		return fmt.Sprintf("%d,%d", start+1, count)
	}
}

func writeDiffLine(out io.Writer, prefix byte, line sourceLine) {
	fmt.Fprintf(out, "%c%s%s", prefix, line.text, line.eol)
	if line.eol == "" {
		fmt.Fprint(out, "\n\\ No newline at end of file\n")
	}
}
