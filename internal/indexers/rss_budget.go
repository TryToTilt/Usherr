package indexers

import (
	"math"
	"time"
)

// RSS budget. The regular check for new releases (RSS sync) reads the latest
// releases of every indexer again and again. An indexer with a daily request
// limit would have its day used up by that before the searches for specific
// wanted titles ran. So for an indexer whose limit is known (set by a person
// or learned from its answers), RSS may use about a third of the day's limit,
// spread over the day by lengthening that indexer's RSS interval, and never
// takes the part of the limit kept for searches. An indexer without a known
// limit is not touched.

const (
	// rssShareDiv: RSS may use one third of the daily limit.
	rssShareDiv = 3
	// searchReservePct is the part of the daily limit that is always left for
	// searches, whatever RSS would like to use (percent).
	searchReservePct = 40
)

// RSSBudget is the most requests a day that RSS may make to an indexer with
// that daily limit: a third of it, rounded, and at least 1 when the limit is
// 2 or more. A limit of 0 (none known) gives 0 and means "no budget needed";
// use RSSPlan.Limited to tell that apart from a limit that leaves no room.
func RSSBudget(limit int) int {
	if limit <= 0 {
		return 0
	}
	b := int(math.Round(float64(limit) / rssShareDiv))
	if b < 1 && limit >= 2 {
		b = 1
	}
	// Never past the part kept for searches.
	return min(b, rssCap(limit))
}

// rssCap is the most requests of a day that RSS and everything else together
// may have used for one more RSS read to be allowed: the limit less the part
// kept for searches.
func rssCap(limit int) int {
	return limit - int(math.Ceil(float64(limit)*searchReservePct/100))
}

// RSSPlan is what the budget says about one indexer for the next RSS run.
type RSSPlan struct {
	Limited bool          // the indexer has a known daily limit
	Allowed bool          // this run may read it now
	Budget  int           // requests of the day RSS may use
	Every   time.Duration // the least time between two RSS runs on it (0 when not limited)
	Used    int           // RSS requests made today
}

func (ls *limitState) initRSS() {
	if ls.rssLast == nil {
		ls.rssLast = map[int64]time.Time{}
		ls.rssUsed = map[int64]*usedNote{}
	}
}

// RSSAdvice says whether an RSS run that makes calls requests to the indexer
// may go ahead now. setLimit is the limit a person set (0 = none). A run
// that is not allowed should leave the indexer out and not note anything.
func (t *Tracker) RSSAdvice(id int64, setLimit, calls int) RSSPlan {
	if t == nil {
		return RSSPlan{Allowed: true}
	}
	calls = max(calls, 1)
	q := t.Quota(id, setLimit)
	if q.Limit <= 0 {
		return RSSPlan{Allowed: true}
	}
	plan := RSSPlan{Limited: true, Budget: RSSBudget(q.Limit)}
	runs := plan.Budget / calls
	if runs < 1 {
		// The limit is too small for a run to fit: RSS leaves it to the searches.
		return plan
	}
	plan.Every = 24 * time.Hour / time.Duration(runs)
	now := t.now()
	day := now.UTC().Format(dayFmt)
	t.mu.Lock()
	t.limits.initRSS()
	last := t.limits.rssLast[id]
	if u := t.limits.rssUsed[id]; u != nil && u.day == day {
		plan.Used = u.n
	}
	t.mu.Unlock()
	plan.Allowed = !q.Blocked &&
		plan.Used+calls <= plan.Budget &&
		q.Used+calls <= rssCap(q.Limit) &&
		(last.IsZero() || now.Sub(last) >= plan.Every)
	return plan
}

// NoteRSS records that an RSS run made calls requests to the indexer now.
func (t *Tracker) NoteRSS(id int64, calls int) {
	if t == nil || id == 0 || calls <= 0 {
		return
	}
	now := t.now()
	day := now.UTC().Format(dayFmt)
	t.mu.Lock()
	defer t.mu.Unlock()
	t.limits.initRSS()
	t.limits.rssLast[id] = now
	u := t.limits.rssUsed[id]
	if u == nil || u.day != day {
		u = &usedNote{day: day}
		t.limits.rssUsed[id] = u
	}
	u.n += calls
}
