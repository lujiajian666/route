package execx

import (
	"context"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	r := &Runner{}
	res, err := r.Run(context.Background(), "/bin/echo", "hello", "world")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(res.Output) != "hello world" {
		t.Fatalf("unexpected output: %q", res.Output)
	}
}

func TestRunFailure(t *testing.T) {
	r := &Runner{}
	_, err := r.Run(context.Background(), "/bin/sh", "-c", "echo boom >&2; exit 3")
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected error containing output, got %v", err)
	}
}
