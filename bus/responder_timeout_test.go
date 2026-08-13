package bus

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// A responder that outruns its deadline is SIGKILLed by exec.CommandContext,
// which reports only "signal: killed" — indistinguishable from a crash or an
// OOM kill, and giving no hint that a timeout is the thing to raise.
func TestRespondErrorNamesTheTimeout(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	err := respondError(ctx, errors.New("signal: killed"), "", 120*time.Second)
	if err == nil {
		t.Fatal("expected an error")
	}
	got := err.Error()
	if !strings.Contains(got, "timed out") {
		t.Errorf("error should say it timed out, got %q", got)
	}
	if !strings.Contains(got, "2m0s") {
		t.Errorf("error should name the deadline it exceeded, got %q", got)
	}
	if strings.Contains(got, "signal: killed") {
		t.Errorf("the raw signal is noise once we know it was a timeout: %q", got)
	}
}

// A genuine failure must keep its stderr — that is the only diagnostic there is.
func TestRespondErrorKeepsStderrForRealFailures(t *testing.T) {
	err := respondError(context.Background(), errors.New("exit status 1"),
		"config file not found", 120*time.Second)

	got := err.Error()
	if !strings.Contains(got, "config file not found") {
		t.Errorf("stderr must survive, got %q", got)
	}
	if strings.Contains(got, "timed out") {
		t.Errorf("a non-deadline failure must not be reported as a timeout: %q", got)
	}
}

func TestRespondTimeoutDefaultIsGenerous(t *testing.T) {
	// The responder reads files, greps and runs git before replying. Two
	// minutes killed real work mid-flight often enough to be noticed.
	if defaultResponderTimeout < 5*time.Minute {
		t.Errorf("default timeout %s is too tight for an investigating responder",
			defaultResponderTimeout)
	}
}
