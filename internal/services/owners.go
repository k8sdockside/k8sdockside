package services

import (
	"context"
	"errors"

	"github.com/rogerwesterbo/k8sdockside/internal/kube"
	"github.com/rogerwesterbo/k8sdockside/internal/session"
)

// errDesktopOnly is what a feature that needs the user's own machine answers in
// the web version: a file picker, a file manager, a terminal emulator, a port
// on localhost. In the web version "this machine" is a pod nobody is sitting
// at, and doing any of those there would help nobody.
var errDesktopOnly = errors.New("this is only available in the desktop app")

// OwnedEvents names, for every event that belongs to one stream, the payload
// field holding that stream's ID. The web version's gateway reads it to deliver
// each such event only to the user who opened the stream -- see
// session.Owners. An event not listed here goes to everyone.
var OwnedEvents = map[string]string{
	TerminalEvent: "sessionId",
	LogEvent:      "streamId",
	SnapshotEvent: "subscriptionId",
	DrainEvent:    "drainId",
	SearchEvent:   "searchId",
	ForwardEvent:  "id",
}

// claimSubscription returns the claim a watcher calls with a new subscription's
// ID, filing it under the caller before its first snapshot is pushed. Nil in
// the desktop app, which has nobody to file it under.
func claimSubscription(caller context.Context, owners *session.Owners, watcher *kube.Watcher) func(string) {
	if owners == nil {
		return nil
	}
	return func(id string) {
		// A freshly minted ID cannot be held by anyone else, so there is no
		// error to report.
		_ = owners.Claim(caller, id, func() { dropSubscription(owners, watcher, id) })
	}
}

// dropSubscription closes a subscription and lets its owner go.
func dropSubscription(owners *session.Owners, watcher *kube.Watcher, id string) {
	watcher.Unsubscribe(id)
	owners.Release(id)
}
