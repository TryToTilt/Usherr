package books

import "sort"

// BestLocalMatch picks the Open Library result that is the book found on
// disk as title and author: the first with the same title (and author, when
// one is known), then the first whose title words are all in the name on
// disk. A name written the other way round ("1984 - George Orwell") is tried
// swapped last. It returns -1 when none fits.
func BestLocalMatch(title, author string, found []Found) int {
	try := func(title, author string) int {
		for _, exact := range []bool{true, false} {
			for i := range found {
				if SameTitle(title, found[i].Title, exact) && (author == "" || SameAuthor(author, found[i].Author)) {
					return i
				}
			}
		}
		return -1
	}
	if i := try(title, author); i >= 0 {
		return i
	}
	if author != "" {
		return try(author, title)
	}
	return -1
}

// RankByTitleAndAuthor orders search results for a book looked for by its
// title and author (Find it myself): the ones by that author first, of
// those the ones with that title first, and otherwise in Open Library's own
// order. A study guide called "1984 George Orwell" is by someone else, so
// Orwell's Nineteen Eighty-Four comes before it.
func RankByTitleAndAuthor(found []Found, title, author string) {
	group := func(f Found) int {
		byAuthor := author == "" || SameAuthor(author, f.Author)
		sameTitle := title != "" && SameTitle(title, f.Title, false)
		switch {
		case byAuthor && sameTitle:
			return 0
		case byAuthor && author != "":
			return 1
		case sameTitle:
			return 2
		}
		return 3
	}
	sort.SliceStable(found, func(i, j int) bool { return group(found[i]) < group(found[j]) })
}
