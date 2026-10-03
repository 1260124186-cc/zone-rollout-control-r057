// zrc-check verifies the internal consistency of a zone rollout control
// state snapshot offline. It is strictly read-only: it never creates,
// locks, modifies, or replaces the snapshot file, and it never starts the
// service. The report on stdout is machine-readable JSON and stable
// across repeated runs over the same file.
//
// Exit status:
//
//	0  snapshot is consistent
//	1  integrity problems were found
//	2  operational failure (bad flags, unreadable or undecodable snapshot)
//	3  scan was canceled before completion (signal or --timeout)
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"solo-0014-zone-rollout-control/internal/domain"
	"solo-0014-zone-rollout-control/internal/snapshotcheck"
)

const (
	exitOK       = 0
	exitIssues   = 1
	exitError    = 2
	exitCanceled = 3
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("zrc-check", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var (
		statePath   string
		maxProblems int
		timeout     time.Duration
	)
	flags.StringVar(&statePath, "state", "data/zrc-state.json", "state snapshot path to verify (opened read-only)")
	flags.IntVar(&maxProblems, "max-problems", 1000, "stop scanning after this many problems (0 means no limit)")
	flags.DurationVar(&timeout, "timeout", 0, "cancel the scan after this duration (0 means no limit)")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "usage: zrc-check [--state PATH] [--max-problems N] [--timeout DURATION]")
		fmt.Fprintln(stderr, "verifies a state snapshot offline and prints a JSON report; never modifies the file")
		fmt.Fprintln(stderr, "exit status: 0 consistent, 1 problems found, 2 operational failure, 3 canceled")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitError
	}
	if flags.NArg() > 0 {
		fmt.Fprintf(stderr, "unexpected argument %q\n", flags.Arg(0))
		flags.Usage()
		return exitError
	}
	if statePath == "" {
		fmt.Fprintln(stderr, "--state must not be empty")
		return exitError
	}
	if maxProblems < 0 {
		fmt.Fprintln(stderr, "--max-problems must not be negative")
		return exitError
	}
	if timeout < 0 {
		fmt.Fprintln(stderr, "--timeout must not be negative")
		return exitError
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	report := check(ctx, statePath, maxProblems)
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		fmt.Fprintf(stderr, "encode report: %v\n", err)
		return exitError
	}
	switch report.Status {
	case snapshotcheck.StatusOK:
		return exitOK
	case snapshotcheck.StatusIssues:
		fmt.Fprintf(stderr, "zrc-check: %d problem(s) found\n", report.ProblemCount)
		return exitIssues
	case snapshotcheck.StatusCanceled:
		fmt.Fprintln(stderr, "zrc-check: scan canceled before completion")
		return exitCanceled
	default:
		fmt.Fprintf(stderr, "zrc-check: %s\n", report.Error)
		return exitError
	}
}

func check(ctx context.Context, statePath string, maxProblems int) snapshotcheck.Report {
	payload, err := os.ReadFile(statePath)
	if err != nil {
		return snapshotcheck.FailureReport(statePath, fmt.Errorf("read snapshot: %w", err))
	}
	var state domain.State
	if err := json.Unmarshal(payload, &state); err != nil {
		return snapshotcheck.FailureReport(statePath, fmt.Errorf("decode snapshot: %w", err))
	}
	return snapshotcheck.Check(ctx, &state, snapshotcheck.Options{
		StatePath:   statePath,
		MaxProblems: maxProblems,
	})
}
