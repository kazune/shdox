//go:build darwin || linux

package main

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestAtomicWritePreservesLinkAndMode(t *testing.T) {
	dir := t.TempDir()
	targetDir := filepath.Join(dir, "target")
	if err := os.Mkdir(targetDir, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(targetDir, "script.sh")
	symlink := filepath.Join(dir, "symlink.sh")
	hardlink := filepath.Join(dir, "hardlink.sh")
	relativeTarget := filepath.Join("target", "script.sh")
	const original = "##\tSummary\nf() { :; }\n"
	const formatted = "## Summary\nf() { :; }\n"
	writeTestFile(t, target, original)
	if err := os.Chmod(target, 0o751); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(relativeTarget, symlink); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(target, hardlink); err != nil {
		t.Fatal(err)
	}
	if status, out, diagnostics := invoke("", "fmt", "-w", symlink); status != 0 || out != "" || diagnostics != "" {
		t.Fatalf("fmt -w = %d, %q, %q", status, out, diagnostics)
	}
	if linkTarget, err := os.Readlink(symlink); err != nil || linkTarget != relativeTarget {
		t.Fatalf("symlink target = %q, %v; want %q", linkTarget, err, relativeTarget)
	}
	for _, file := range []struct{ path, want string }{{target, formatted}, {hardlink, original}} {
		if data, err := os.ReadFile(file.path); err != nil || string(data) != file.want {
			t.Errorf("%s = %q, %v; want %q", file.path, data, err, file.want)
		}
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o751 {
		t.Errorf("permissions = %o; want 751", info.Mode().Perm())
	}
	if status, out, diagnostics := invoke("", "fmt", "-w", symlink); status != 0 || out != "" || diagnostics != "" {
		t.Fatalf("fmt -w on formatted input = %d, %q, %q", status, out, diagnostics)
	}
	if after, err := os.Stat(target); err != nil || !os.SameFile(info, after) {
		t.Errorf("fmt -w replaced an unchanged file, stat error = %v", err)
	}
}

func TestAtomicWriteFailureKeepsOriginal(t *testing.T) {
	const fileEnv = "SHDOX_TEST_LIMITED_WRITE_FILE"
	if target := os.Getenv(fileEnv); target != "" {
		signal.Ignore(syscall.SIGXFSZ)
		if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &syscall.Rlimit{Cur: 12, Max: 12}); err != nil {
			t.Fatal(err)
		}
		os.Exit(run([]string{"fmt", "-w", target}, os.Stdin, os.Stdout, os.Stderr))
	}

	dir := t.TempDir()
	target := filepath.Join(dir, "script.sh")
	const original = "##\tSummary\nf() { :; }\n"
	writeTestFile(t, target, original)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(executable, "-test.run=^TestAtomicWriteFailureKeepsOriginal$")
	cmd.Env = append(os.Environ(), fileEnv+"="+target)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	stdout, err := cmd.Output()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 2 || len(stdout) != 0 ||
		!strings.HasPrefix(stderr.String(), target+": ") || !strings.Contains(stderr.String(), syscall.EFBIG.Error()) {
		t.Fatalf("fmt -w = %v, %q, %q", err, stdout, &stderr)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != original {
		t.Errorf("file = %q, %v; want %q", data, err, original)
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 1 {
		t.Errorf("directory entries = %v, %v; want only the original file", entries, err)
	}
}
