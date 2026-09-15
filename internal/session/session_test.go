package session

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func as(id string, admin bool, client string) context.Context {
	ctx := WithUser(context.Background(), User{ID: id, Username: id, Admin: admin})
	return WithClient(ctx, client)
}

func TestRequireAdmin(t *testing.T) {
	if err := RequireAdmin(context.Background()); err != nil {
		t.Fatalf("a call made for nobody is the desktop's and must pass, got %v", err)
	}
	if err := RequireAdmin(as("u1", true, "")); err != nil {
		t.Fatalf("an admin must pass, got %v", err)
	}
	if err := RequireAdmin(as("u2", false, "")); !errors.Is(err, ErrNotAdmin) {
		t.Fatalf("a user who is not an admin must be refused, got %v", err)
	}
}

func TestClaimAndAllowed(t *testing.T) {
	o := NewOwners()
	alice, bob := as("alice", false, "tab-a"), as("bob", false, "tab-b")

	if err := o.Claim(alice, "term-1", nil); err != nil {
		t.Fatal(err)
	}
	if !o.Allowed(alice, "term-1") {
		t.Fatal("the owner must be allowed")
	}
	if o.Allowed(bob, "term-1") {
		t.Fatal("another user must not be allowed")
	}
	if o.Allowed(alice, "term-2") {
		t.Fatal("an id nobody claimed must be refused to a user")
	}
	if !o.Allowed(context.Background(), "term-1") {
		t.Fatal("a call made for nobody is the desktop's and is allowed")
	}
	if user, ok := o.UserOf("term-1"); !ok || user != "alice" {
		t.Fatalf("UserOf = %q, %v", user, ok)
	}

	if err := o.Claim(bob, "term-1", nil); !errors.Is(err, ErrTaken) {
		t.Fatalf("claiming someone else's id must fail, got %v", err)
	}
	if err := o.Claim(alice, "term-1", nil); err != nil {
		t.Fatalf("reclaiming your own id must work, got %v", err)
	}
}

func TestNilOwnersIsTheDesktop(t *testing.T) {
	var o *Owners
	if err := o.Claim(as("alice", false, ""), "x", nil); err != nil {
		t.Fatal(err)
	}
	if !o.Allowed(as("bob", false, ""), "x") {
		t.Fatal("a nil registry must allow everything")
	}
	o.Release("x")
	o.AbandonUser("alice")
	if _, ok := o.UserOf("x"); ok {
		t.Fatal("a nil registry knows no owners")
	}
}

func TestReleaseKeepsTheOwnerForTheGracePeriod(t *testing.T) {
	o := NewOwners()
	o.grace = 20 * time.Millisecond
	alice := as("alice", false, "tab-a")

	if err := o.Claim(alice, "logs-1", nil); err != nil {
		t.Fatal(err)
	}
	o.Release("logs-1")
	if user, ok := o.UserOf("logs-1"); !ok || user != "alice" {
		t.Fatal("a released id must keep its owner until the grace period is over")
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := o.UserOf("logs-1"); !ok {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("a released id must be forgotten after the grace period")
}

func TestAbandonRunsCleanupsOnce(t *testing.T) {
	o := NewOwners()
	var tabA, tabB, other atomic.Int32
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(o.Claim(as("alice", false, "tab-a"), "term-1", func() { tabA.Add(1) }))
	must(o.Claim(as("alice", false, "tab-b"), "term-2", func() { tabB.Add(1) }))
	must(o.Claim(as("bob", false, "tab-c"), "term-3", func() { other.Add(1) }))

	o.AbandonClient("tab-a")
	o.AbandonClient("tab-a")
	if tabA.Load() != 1 || tabB.Load() != 0 || other.Load() != 0 {
		t.Fatalf("abandoning a tab must close that tab's streams once: a=%d b=%d other=%d", tabA.Load(), tabB.Load(), other.Load())
	}

	o.AbandonUser("alice")
	if tabA.Load() != 1 || tabB.Load() != 1 || other.Load() != 0 {
		t.Fatalf("abandoning a user must close the rest of that user's streams: a=%d b=%d other=%d", tabA.Load(), tabB.Load(), other.Load())
	}
}

func TestReleasedStreamsAreNotCleanedUp(t *testing.T) {
	o := NewOwners()
	var closed atomic.Int32
	if err := o.Claim(as("alice", false, "tab-a"), "drain-1", func() { closed.Add(1) }); err != nil {
		t.Fatal(err)
	}
	o.Release("drain-1")
	o.AbandonClient("tab-a")
	if closed.Load() != 0 {
		t.Fatal("a stream that has already ended must not be closed again")
	}
}

func TestCleanupMayReleaseWithoutDeadlock(t *testing.T) {
	o := NewOwners()
	done := make(chan struct{})
	if err := o.Claim(as("alice", false, "tab-a"), "sub-1", func() { o.Release("sub-1") }); err != nil {
		t.Fatal(err)
	}
	go func() {
		o.AbandonClient("tab-a")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("a cleanup that releases its own claim must not deadlock")
	}
}
