package handlers

import (
	"testing"
	"time"
)

func TestFailureLimiter(t *testing.T) {
	l := newFailureLimiter(3, time.Hour)
	for i := 0; i < 3; i++ {
		if !l.allow("a") {
			t.Fatalf("blocked after %d failures", i)
		}
		l.fail("a")
	}
	if l.allow("a") {
		t.Fatal("not blocked after the limit")
	}
	if !l.allow("b") {
		t.Fatal("another key is blocked")
	}
	l.reset("a")
	if !l.allow("a") {
		t.Fatal("still blocked after reset")
	}

	short := newFailureLimiter(1, time.Millisecond)
	short.fail("x")
	time.Sleep(5 * time.Millisecond)
	if !short.allow("x") {
		t.Fatal("window did not expire")
	}
}
