package server

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRetryWithBackoff_SucceedsAfterTransientFailures(t *testing.T) {
	attempts := 0
	err := retryWithBackoff(context.Background(), time.Millisecond, 5*time.Millisecond, 500*time.Millisecond, func() error {
		attempts++
		if attempts < 3 {
			return errors.New("transient")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("retryWithBackoff() error = %v", err)
	}
	if attempts != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}
}

func TestRetryWithBackoff_SucceedsFirstTry(t *testing.T) {
	attempts := 0
	err := retryWithBackoff(context.Background(), time.Millisecond, 5*time.Millisecond, 500*time.Millisecond, func() error {
		attempts++
		return nil
	})
	if err != nil {
		t.Fatalf("retryWithBackoff() error = %v", err)
	}
	if attempts != 1 {
		t.Errorf("attempts = %d, want 1", attempts)
	}
}

func TestRetryWithBackoff_GivesUpAfterBudget(t *testing.T) {
	wantErr := errors.New("permanent")
	attempts := 0

	err := retryWithBackoff(context.Background(), time.Millisecond, 5*time.Millisecond, 30*time.Millisecond, func() error {
		attempts++
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("retryWithBackoff() error = %v, want %v", err, wantErr)
	}
	if attempts < 2 {
		t.Errorf("expected multiple attempts before giving up, got %d", attempts)
	}
}

func TestRetryWithBackoff_ReturnsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	attempts := 0
	done := make(chan error, 1)
	go func() {
		done <- retryWithBackoff(ctx, 200*time.Millisecond, 200*time.Millisecond, time.Minute, func() error {
			attempts++
			return errors.New("always fails")
		})
	}()

	// Let the first attempt happen, then cancel while it's sleeping before retry.
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("retryWithBackoff() error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("retryWithBackoff did not return promptly after context cancellation")
	}
	if attempts < 1 {
		t.Error("expected at least one attempt before cancellation")
	}
}
