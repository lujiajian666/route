// Package execx 执行本机命令（argv 数组，不经过 shell）。
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

type Result struct {
	Command  string `json:"command"`
	Output   string `json:"output"`
	ExitCode int    `json:"exitCode"`
}

type Runner struct {
	Timeout time.Duration
}

func (r *Runner) Run(ctx context.Context, bin string, args ...string) (Result, error) {
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	res := Result{Command: strings.Join(append([]string{bin}, args...), " ")}

	cmd := exec.CommandContext(cctx, bin, args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	err := cmd.Run()
	res.Output = strings.TrimSpace(buf.String())
	res.ExitCode = exitCode(err)

	if cctx.Err() == context.DeadlineExceeded {
		return res, fmt.Errorf("命令超时：%s", res.Command)
	}
	if err != nil {
		detail := firstLine(res.Output)
		if detail == "" {
			detail = err.Error()
		}
		if errors.Is(err, exec.ErrNotFound) {
			return res, fmt.Errorf("找不到命令 %s", bin)
		}
		return res, fmt.Errorf("%s 执行失败：%s", res.Command, detail)
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
