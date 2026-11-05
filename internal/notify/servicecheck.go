package notify

// Usherr: Service checks (internal/servicecheck). Usherr asks the addresses an
// administrator listed every so often and says when one stops answering and
// when it answers again, so a separate watchdog is not needed.

import (
	"fmt"
	"time"
)

// The two service check events. Both are opt-in (see OptIn): a target only
// gets them when it was set up to.
const (
	EventServiceDown = "servicedown" // a watched service stopped answering
	EventServiceUp   = "serviceup"   // a watched service answers again
)

// ComposeServiceDown is the message for a watched service that stopped
// answering. fails is how many checks in a row failed, since is when the first
// of them failed, reason what went wrong in plain words ("connection
// refused"). The address is never in the message: it may hold a key.
func ComposeServiceDown(name string, fails int, since time.Time, reason string, links Links, at time.Time) Event {
	lead := fmt.Sprintf("%s has not answered for %d checks in a row", name, fails)
	if !since.IsZero() && at.After(since) {
		lead += fmt.Sprintf(" (%s)", outageText(at.Sub(since)))
	}
	if r := plainReason(reason); r != "" {
		lead += ": " + r
	}
	ev := ComposeText(EventServiceDown, name+" is down", lead+".", links, at)
	ev.LinkURL = links.to("/settings/service-checks")
	return ev
}

// ComposeServiceUp is the message for a watched service that answers again
// after its "down" message.
func ComposeServiceUp(name string, outage time.Duration, links Links, at time.Time) Event {
	ev := ComposeText(EventServiceUp, name+" is back", fmt.Sprintf("%s is answering again after %s.", name, outageText(outage)), links, at)
	ev.LinkURL = links.to("/settings/service-checks")
	return ev
}
