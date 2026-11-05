package servicecheck

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// Notifier sends the "down" and "back" messages.
type Notifier interface {
	Down(c Check, at time.Time)
	Back(c Check, outage time.Duration, at time.Time)
}

// Runner checks the services that are due. Run it often (every 15 seconds);
// each service keeps its own interval.
type Runner struct {
	Repo   *Repo
	Client *http.Client // default NewClient()
	Notify Notifier     // may be nil: the state is kept, nothing is sent
	Now    func() time.Time

	mu        sync.Mutex // one round at a time
	lastPrune time.Time
}

// slack lets a check that is a moment early count as due, so a 60-second
// check run by a 15-second tick is not pushed to 75 seconds.
const slack = 3 * time.Second

// parallel is how many services are asked at the same time.
const parallel = 8

func (r *Runner) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *Runner) client() *http.Client {
	if r.Client != nil {
		return r.Client
	}
	return NewClient()
}

// Due reports whether c should be checked at now.
func Due(c Check, now time.Time) bool {
	return c.Enabled && (c.Checked.IsZero() || now.Sub(c.Checked) >= c.Interval-slack)
}

// RunDue checks every enabled service whose interval has passed. A round
// that is still going (a slow service) makes the next one wait its turn.
func (r *Runner) RunDue(ctx context.Context) {
	if !r.mu.TryLock() {
		return
	}
	defer r.mu.Unlock()
	now := r.now()
	list, err := r.Repo.List(ctx)
	if err != nil {
		slog.Warn("service checks: could not read the list", "err", err)
		return
	}
	var due []Check
	for _, c := range list {
		if Due(c, now) {
			due = append(due, c)
		}
	}
	client := r.client()
	sem := make(chan struct{}, parallel)
	var wg sync.WaitGroup
	for _, c := range due {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			res := Probe(ctx, client, c, r.Now)
			if _, err := r.apply(ctx, c, res); err != nil && ctx.Err() == nil {
				slog.Warn("service checks: could not save a check", "service", c.Name, "err", err)
			}
		}()
	}
	wg.Wait()
	if now.Sub(r.lastPrune) >= time.Hour {
		r.lastPrune = now
		if err := r.Repo.Prune(ctx, now); err != nil {
			slog.Warn("service checks: could not forget old results", "err", err)
		}
	}
}

// CheckNow checks one service at once (the "Check now" button) and saves
// the result like any other check, messages included.
func (r *Runner) CheckNow(ctx context.Context, id int64) (Check, error) {
	c, err := r.Repo.Get(ctx, id)
	if err != nil {
		return c, err
	}
	return r.apply(ctx, c, Probe(ctx, r.client(), c, r.Now))
}

// Try asks an address once without saving anything (the "Try it" button).
func (r *Runner) Try(ctx context.Context, c Check) Result {
	return Probe(ctx, r.client(), c, r.Now)
}

// apply records one result and sends the message it calls for.
func (r *Runner) apply(ctx context.Context, c Check, res Result) (Check, error) {
	next, v := Step(c, res)
	var change *Change
	switch v {
	case WentDown:
		change = &Change{At: res.At, State: StateDown, Detail: fmt.Sprintf("%d failed checks in a row: %s", next.Fails, res.Error)}
	case CameBack:
		change = &Change{At: res.At, State: StateUp, Detail: "Answering again after " + durationText(c.Outage(res.At)) + "."}
	}
	if err := r.Repo.Record(ctx, next, res, change); err != nil {
		return next, err
	}
	if r.Notify != nil {
		switch v {
		case WentDown:
			slog.Info("service checks: a service is down", "service", c.Name, "reason", res.Error)
			r.Notify.Down(next, res.At)
		case CameBack:
			slog.Info("service checks: a service is back", "service", c.Name)
			r.Notify.Back(next, c.Outage(res.At), res.At)
		}
	}
	return next, nil
}

// durationText is a short "12 minutes" or "1 hour 5 minutes".
func durationText(d time.Duration) string {
	mins := int((d + 30*time.Second) / time.Minute)
	switch {
	case mins < 1:
		return "less than a minute"
	case mins == 1:
		return "1 minute"
	case mins < 60:
		return fmt.Sprintf("%d minutes", mins)
	}
	h, m := mins/60, mins%60
	out := fmt.Sprintf("%d hours", h)
	if h == 1 {
		out = "1 hour"
	}
	switch {
	case m == 1:
		out += " 1 minute"
	case m > 1:
		out += fmt.Sprintf(" %d minutes", m)
	}
	return out
}
