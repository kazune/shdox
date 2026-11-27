package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

type options struct {
	command  string
	language syntax.LangVariant
	diff     bool
	write    bool
	files    []string
}

func parseArgs(args []string) (options, error) {
	var opts options
	if len(args) == 0 {
		return opts, fmt.Errorf("expected check, fmt, or generate")
	}
	opts.command = args[0]
	opts.language = syntax.LangAuto
	switch opts.command {
	case "check", "fmt", "generate":
	default:
		return opts, fmt.Errorf("unknown command %q", opts.command)
	}

	stdin, endOfOptions := false, false
	for i := 1; i < len(args); i++ {
		arg := args[i]
		option, value, hasValue := strings.Cut(arg, "=")
		switch {
		case !endOfOptions && arg == "--":
			endOfOptions = true
		case !endOfOptions && arg != "-" && strings.HasPrefix(arg, "-"):
			switch {
			case option == "--lang":
				if !hasValue {
					i++
					if i == len(args) || strings.HasPrefix(args[i], "-") {
						return opts, fmt.Errorf("--lang requires a value")
					}
					value = args[i]
				}
				switch value {
				case "bash", "posix", "mksh", "bats", "zsh":
					if err := opts.language.Set(value); err != nil {
						return opts, err
					}
				default:
					return opts, fmt.Errorf("unknown language %q", value)
				}
			case opts.command == "fmt" && arg == "-d":
				opts.diff = true
			case opts.command == "fmt" && arg == "-w":
				opts.write = true
			default:
				return opts, fmt.Errorf("unknown option %q for %s", arg, opts.command)
			}
		default:
			if arg == "-" {
				if stdin {
					return opts, fmt.Errorf("standard input can only be specified once")
				}
				stdin = true
			}
			opts.files = append(opts.files, arg)
		}
	}
	switch {
	case len(opts.files) == 0:
		return opts, fmt.Errorf("expected at least one input file")
	case opts.diff && opts.write:
		return opts, fmt.Errorf("-d and -w cannot be combined")
	case opts.write && stdin:
		return opts, fmt.Errorf("-w cannot be used with standard input")
	}
	return opts, nil
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	opts, err := parseArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "shdox: %v\n", err)
		return 2
	}

	status := 0
	var results []analysis
	for _, name := range opts.files {
		var data []byte
		if name == "-" {
			data, err = io.ReadAll(stdin)
		} else {
			data, err = os.ReadFile(name)
		}
		if err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", name, ioError(err))
			status = 2
			continue
		}
		result, issues := analyze(name, string(data), opts.language)
		for _, issue := range issues {
			if issue.line != 0 {
				fmt.Fprintf(stderr, "%s:%d:%d: %s\n", name, issue.line, issue.column, issue.message)
			} else {
				fmt.Fprintln(stderr, issue.message)
			}
			if opts.command == "check" && issue.violation {
				status = max(status, 1)
			} else {
				status = 2
			}
		}
		if opts.command != "check" {
			results = append(results, result)
		}
	}
	if status != 0 {
		return status
	}

	output := bufio.NewWriter(stdout)
	wroteSection := false
	for i, result := range results {
		name := opts.files[i]
		switch {
		case opts.command == "generate":
			for _, doc := range result.documents {
				if wroteSection {
					output.WriteByte('\n')
				}
				wroteSection = true
				writeMarkdown(output, doc)
			}
		case opts.diff:
			if writeDiff(output, name, result.lines, result.formatted) {
				status = 1
			}
		case opts.write:
			if !slices.Equal(result.lines, result.formatted) {
				var source strings.Builder
				writeSource(&source, result.formatted)
				if err := os.WriteFile(name, []byte(source.String()), 0o666); err != nil {
					fmt.Fprintf(stderr, "%s: %v\n", name, ioError(err))
					return 2
				}
			}
		default:
			writeSource(output, result.formatted)
		}
	}
	if err := output.Flush(); err != nil {
		fmt.Fprintf(stderr, "shdox: writing standard output: %v\n", ioError(err))
		return 2
	}
	return status
}

func ioError(err error) error {
	if pathErr, ok := err.(*os.PathError); ok {
		return pathErr.Err
	}
	return err
}
