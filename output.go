package main

import (
	"cmp"
	"fmt"
	"io"
	"slices"
	"strings"
)

func writeSource(out io.StringWriter, lines []sourceLine) {
	for _, line := range lines {
		out.WriteString(line.text)
		out.WriteString(line.eol)
	}
}

func writeMarkdown(out io.Writer, doc document) {
	fmt.Fprintf(out, "## %s\n\n%s\n", codeSpan(doc.name), doc.summary)
	for _, paragraph := range doc.details {
		fmt.Fprintf(out, "\n%s\n", strings.Join(paragraph, "\n"))
	}
	for _, section := range orderedTagSections(doc.tags) {
		fmt.Fprintf(out, "\n### %s\n\n", section.heading)
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

// Formatting preserves line count, so matching lines are at the same index.
func writeDiff(out io.Writer, name string, before, after []sourceLine) bool {
	const contextLines = 3
	var changed []int
	for i := range before {
		if before[i] != after[i] {
			changed = append(changed, i)
		}
	}
	if len(changed) == 0 {
		return false
	}
	fmt.Fprintf(out, "--- %s\n+++ %s\n", name, name)

	for len(changed) != 0 {
		first := changed[0]
		count := 1
		for count < len(changed) && changed[count]-changed[count-1] <= 2*contextLines+1 {
			count++
		}
		start := max(0, first-contextLines)
		end := min(len(before), changed[count-1]+contextLines+1)
		hunkRange := fmt.Sprintf("%d", start+1)
		if end-start != 1 {
			hunkRange += fmt.Sprintf(",%d", end-start)
		}
		fmt.Fprintf(out, "@@ -%s +%s @@\n", hunkRange, hunkRange)
		for i := start; i < end; {
			if before[i] == after[i] {
				writeDiffLine(out, ' ', before[i])
				i++
				continue
			}
			j := i + 1
			for j < end && before[j] != after[j] {
				j++
			}
			for _, line := range before[i:j] {
				writeDiffLine(out, '-', line)
			}
			for _, line := range after[i:j] {
				writeDiffLine(out, '+', line)
			}
			i = j
		}
		changed = changed[count:]
	}
	return true
}

func writeDiffLine(out io.Writer, prefix byte, line sourceLine) {
	fmt.Fprintf(out, "%c%s%s", prefix, line.text, line.eol)
	if line.eol == "" {
		fmt.Fprint(out, "\n\\ No newline at end of file\n")
	}
}
