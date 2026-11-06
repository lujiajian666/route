package execx

import (
	"context"
	"strings"
	"testing"
)

func TestDryRunPrivileged(t *testing.T) {
	r := &Runner{Sudo: true, DryRun: true}
	res, err := r.Privileged(context.Background(), "route", "add", "-host", "5.5.2.13", "192.168.100.254")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Simulated {
		t.Fatal("expected simulated result")
	}
	if res.Command != "sudo route add -host 5.5.2.13 192.168.100.254" {
		t.Fatalf("unexpected command: %s", res.Command)
	}
}

func TestReadRunsCommand(t *testing.T) {
	r := &Runner{}
	res, err := r.Read(context.Background(), "/bin/echo", "hello", "world")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(res.Output) != "hello world" {
		t.Fatalf("unexpected output: %q", res.Output)
	}
	if res.ExitCode != 0 {
		t.Fatalf("unexpected exit code: %d", res.ExitCode)
	}
}

func TestCommandFailureIncludesOutput(t *testing.T) {
	r := &Runner{}
	_, err := r.Read(context.Background(), "/bin/sh", "-c", "echo boom >&2; exit 3")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected stderr in error, got %v", err)
	}
}

func TestSudoProbeNoSudo(t *testing.T) {
	r := &Runner{Sudo: false}
	ok, msg := r.SudoProbe(context.Background(), "route", "-n", "get", "127.0.0.1")
	if !ok || msg != "" {
		t.Fatalf("expected implicit success without sudo, got ok=%v msg=%q", ok, msg)
	}
}
