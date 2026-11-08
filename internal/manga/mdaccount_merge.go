package manga

// Two-way sync without a winner by default: each side is compared with what both
// had at the last sync, so a change on either side is kept, and something nobody
// changed stays as it is. A change on both sides at once: reading wins over
// unreading, following over unfollowing, and for a status, Usherr's.

// MergeSets merges a set kept here and one kept on MangaDex, given what both had
// last time. Only keys in scope are merged; for the rest, MangaDex's word stands.
func MergeSets(here, there, synced, scope map[string]bool) map[string]bool {
	out := map[string]bool{}
	for k := range there {
		if there[k] && !scope[k] {
			out[k] = true
		}
	}
	for k := range scope {
		if !scope[k] {
			continue
		}
		h, t, s := here[k], there[k], synced[k]
		if (h && t) || ((h || t) && !s) {
			out[k] = true
		}
	}
	return out
}

// ReadPlan is what a read-mark sync sends to MangaDex.
type ReadPlan struct {
	Merged map[string]bool // chapter numbers read, from now on
	Scope  map[string]bool // the numbers both sides can say something about
	Read   []string        // chapter ids to mark read there
	Unread []string        // chapter ids to mark unread there
}

// PlanReads merges one series' read marks. there are the chapter ids MangaDex says
// are read; m says which number each is; ids it doesn't know are left alone.
func PlanReads(lr LocalReading, there []string, synced map[string]bool, m DexChapterMap) ReadPlan {
	thereNumbers := map[string]bool{}
	thereIDs := map[string][]string{}
	for _, id := range there {
		if n, ok := m.Number[id]; ok {
			thereNumbers[n] = true
			thereIDs[n] = append(thereIDs[n], id)
		}
	}
	// A number Usherr shows but can't name on MangaDex (a chapter from a file)
	// stays Usherr's own business.
	scope := map[string]bool{}
	for n := range lr.Shown {
		if len(m.IDs[n]) > 0 {
			scope[n] = true
		}
	}
	plan := ReadPlan{Merged: MergeSets(lr.Read, thereNumbers, synced, scope), Scope: scope}
	for _, n := range SortedKeys(plan.Merged) {
		if !thereNumbers[n] && len(m.IDs[n]) > 0 {
			plan.Read = append(plan.Read, m.IDs[n][0])
		}
	}
	for _, n := range SortedKeys(thereNumbers) {
		if !plan.Merged[n] {
			plan.Unread = append(plan.Unread, thereIDs[n]...)
		}
	}
	return plan
}

// MergeStatus merges a reading status: Usherr's change first, then MangaDex's.
func MergeStatus(here, there, synced string) string {
	if here != synced {
		return here
	}
	return there
}
