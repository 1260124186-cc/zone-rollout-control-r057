// Command zrcverify checks a zone rollout control state snapshot offline.
// It never creates, modifies, locks, or replaces the snapshot file. The
// report is written to stdout as JSON; the exit status summarizes it:
//
//	0  snapshot is consistent
//	1  scan completed and found integrity problems
//	2  scan did not complete (unreadable or oversized snapshot, decode
//	   failure, problem limit reached, timeout, or cancellation)
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"solo-0014-zone-rollout-control/internal/verify"
)

func main() {
	os.Exit(run())
}

func run() int {
	var (
		snapshotPath string
		maxBytes     int64
		maxProblems  int
		timeout      time.Duration
		compact      bool
	)
	flag.StringVar(&snapshotPath, "state", "data/zrc-state.json", "state snapshot path to verify (read-only)")
	flag.Int64Var(&maxBytes, "max-bytes", verify.DefaultMaxBytes, "maximum snapshot size in bytes")
	flag.IntVar(&maxProblems, "max-problems", verify.DefaultMaxProblems, "maximum number of problems to collect")
	flag.DurationVar(&timeout, "timeout", 0, "overall scan timeout (0 disables)")
	flag.BoolVar(&compact, "compact", false, "write the report as a single JSON line")
	flag.Parse()
	if maxBytes < 1 {
		fmt.Fprintln(os.Stderr, "max-bytes must be positive")
		return 2
	}
	if maxProblems < 1 {
		fmt.Fprintln(os.Stderr, "max-problems must be positive")
		return 2
	}
	if timeout < 0 {
		fmt.Fprintln(os.Stderr, "timeout must not be negative")
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	report := verify.CheckFile(ctx, snapshotPath, verify.Options{
		MaxBytes:    maxBytes,
		MaxProblems: maxProblems,
	})
	var (
		payload []byte
		err     error
	)
	if compact {
		payload, err = json.Marshal(report)
	} else {
		payload, err = json.MarshalIndent(report, "", "  ")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, fmt.Errorf("encode report: %w", err))
		return 2
	}
	payload = append(payload, '\n')
	if _, err := os.Stdout.Write(payload); err != nil {
		fmt.Fprintln(os.Stderr, fmt.Errorf("write report: %w", err))
		return 2
	}
	switch report.Status {
	case verify.StatusOK:
		return 0
	case verify.StatusProblems:
		return 1
	default:
		return 2
	}
}
