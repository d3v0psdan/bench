package supervisor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// maxExecOutput caps how much output Exec keeps; init tools can be chatty
// and only the tail matters for an error message.
const maxExecOutput = 64 << 10

// execWaitDelay bounds how long Exec waits for output after the tool exits
// or is killed (a daemonized grandchild can keep the pipes open).
const execWaitDelay = 5 * time.Second

// Exec runs a short-lived tool (initdb, mysqld --initialize, mysqladmin
// shutdown) to completion: same window/env handling as supervised
// children, killed when ctx ends, never left running. Health and Shutdown
// in spec are ignored; LogFile, when set, also receives the output.
// The combined output is returned (tail-truncated) alongside any error.
func Exec(ctx context.Context, spec Spec) (string, error) {
	if spec.Command == "" {
		return "", errors.New("exec spec needs Command")
	}
	cmd := exec.CommandContext(ctx, spec.Command, spec.Args...)
	cmd.Dir = spec.Dir
	if len(spec.Env) > 0 {
		cmd.Env = append(os.Environ(), spec.Env...)
	}
	cmd.SysProcAttr = sysProcAttr()
	// On cancel, kill the tool's whole process group (Unix), and don't wait
	// forever on output pipes a grandchild may still hold open.
	cmd.Cancel = func() error {
		killHard(cmd.Process)
		return nil
	}
	cmd.WaitDelay = execWaitDelay
	var out tailBuffer
	// One writer for both streams: os/exec then serializes the writes.
	var w io.Writer = &out
	if spec.Output != nil {
		w = io.MultiWriter(&out, spec.Output)
	}
	cmd.Stdout = w
	cmd.Stderr = w
	err := cmd.Run()
	text := strings.TrimSpace(out.String())
	if spec.LogFile != "" {
		appendLog(spec.LogFile, spec.Command, spec.Args, text)
	}
	if err != nil {
		if ctx.Err() != nil {
			return text, fmt.Errorf("%s: %w", spec.Command, ctx.Err())
		}
		return text, fmt.Errorf("%s: %w", spec.Command, err)
	}
	return text, nil
}

// appendLog records a one-shot run in the service log. Best effort: a
// log write failure must not fail the tool run it describes.
func appendLog(path, command string, args []string, output string) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "$ %s %s\n%s\n", command, strings.Join(args, " "), output)
}

// tailBuffer keeps the last maxExecOutput bytes written to it.
type tailBuffer struct{ bytes.Buffer }

func (b *tailBuffer) Write(p []byte) (int, error) {
	n, _ := b.Buffer.Write(p)
	if over := b.Len() - maxExecOutput; over > 0 {
		b.Next(over)
	}
	return n, nil
}
