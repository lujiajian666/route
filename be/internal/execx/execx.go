// Package execx provides a small, safe wrapper around os/exec.
//
// Commands are always passed as argument vectors (never through a shell), and
// mutating routing commands can optionally be prefixed with `sudo -n`.
package execx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Result is the outcome of a command invocation.
type Result struct {
	Command   string `json:"command"`
	Output    string `json:"output"`
	ExitCode  int    `json:"exitCode"`
	Simulated bool   `json:"simulated"`
}

// Runner executes local commands.
type Runner struct {
	// Sudo prefixes privileged commands with `sudo -n`.
	Sudo bool
	// SudoBin is the sudo executable (defaults to "sudo").
	SudoBin string
	// DryRun skips privileged commands and reports a simulated success.
	DryRun bool
	// Timeout bounds every command invocation.
	Timeout time.Duration
}

func (r *Runner) timeout() time.Duration {
	if r.Timeout <= 0 {
		return 10 * time.Second
	}
	return r.Timeout
}

func (r *Runner) sudoBin() string {
	if strings.TrimSpace(r.SudoBin) == "" {
		return "sudo"
	}
	return r.SudoBin
}

// CommandString renders a human readable command line for logs and the UI.
func (r *Runner) CommandString(sudo bool, bin string, args ...string) string {
	parts := make([]string, 0, len(args)+2)
	if sudo && r.Sudo {
		parts = append(parts, "sudo")
	}
	parts = append(parts, bin)
	parts = append(parts, args...)
	return strings.Join(parts, " ")
}

// Read runs a read-only command.
func (r *Runner) Read(ctx context.Context, bin string, args ...string) (Result, error) {
	return r.exec(ctx, bin, args, false, false)
}

// Privileged runs a command that mutates the routing table. In dry-run mode the
// command is not executed and a simulated result is returned.
func (r *Runner) Privileged(ctx context.Context, bin string, args ...string) (Result, error) {
	return r.exec(ctx, bin, args, true, true)
}

// SudoProbe checks whether privileged commands can run non-interactively.
func (r *Runner) SudoProbe(ctx context.Context, bin string, args ...string) (bool, string) {
	if !r.Sudo {
		return true, ""
	}
	rr := *r
	rr.DryRun = false
	res, err := rr.exec(ctx, bin, args, true, false)
	out := strings.TrimSpace(res.Output)
	if err != nil {
		// A failing route/ip command still proves sudo worked; only sudo's own
		// diagnostics (or a missing binary) mean the probe failed.
		if res.ExitCode == -1 || strings.Contains(out, "sudo:") {
			if out == "" {
				out = err.Error()
			}
			return false, out
		}
		return true, ""
	}
	return true, ""
}

func (r *Runner) exec(ctx context.Context, bin string, args []string, sudo, canSimulate bool) (Result, error) {
	command := r.CommandString(sudo, bin, args...)
	res := Result{Command: command}

	if sudo && r.DryRun && canSimulate {
		res.Simulated = true
		return res, nil
	}

	name := bin
	execArgs := args
	if sudo && r.Sudo {
		name = r.sudoBin()
		execArgs = append([]string{"-n", bin}, args...)
	}

	cctx, cancel := context.WithTimeout(ctx, r.timeout())
	defer cancel()

	cmd := exec.CommandContext(cctx, name, execArgs...)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	err := cmd.Run()
	res.Output = strings.TrimSpace(buf.String())
	res.ExitCode = exitCode(err)

	if cctx.Err() == context.DeadlineExceeded {
		return res, fmt.Errorf("命令执行超时（%s）：%s", r.timeout(), command)
	}
	if err != nil {
		detail := firstLine(res.Output)
		if detail == "" {
			detail = err.Error()
		}
		if errors.Is(err, exec.ErrNotFound) {
			return res, fmt.Errorf("找不到命令 %s", name)
		}
		return res, fmt.Errorf("%s 执行失败：%s", command, detail)
	}
	return res, nil
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}
