package cleanup

import (
	"fmt"
	"sort"
)

// ScanAll is Scan over several areas, for when downloads are worked on in more
// than one folder (the downloads folder and a temporary download folder). Each
// area keeps its own rules: an entry is only ever judged and removed inside the
// area it was found in. Items are sorted by path.
func ScanAll(areas []Area, classify Classify) ([]Item, error) {
	var all []Item
	var firstErr error
	for i, a := range areas {
		items, err := Scan(a, classify)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		for _, it := range items {
			it.area = i
			all = append(all, it)
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].Path < all[j].Path })
	return all, firstErr
}

// RemoveAll removes items found by ScanAll, each one with the rules of the area
// it was found in. It carries on past a failure and returns the first error.
func RemoveAll(areas []Area, items []Item) ([]Item, error) {
	var removed []Item
	var firstErr error
	for _, it := range items {
		if it.area < 0 || it.area >= len(areas) {
			if firstErr == nil {
				firstErr = fmt.Errorf("remove %s: %w", it.Path, ErrOutside)
			}
			continue
		}
		done, err := Remove(areas[it.area], []Item{it})
		if err != nil && firstErr == nil {
			firstErr = err
		}
		removed = append(removed, done...)
	}
	return removed, firstErr
}
