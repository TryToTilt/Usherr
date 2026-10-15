package api

// Home fork: one series, several editions. A library can follow the same
// series twice, in two editions: Vagabond in VizBig volumes (three standard
// volumes each) and in its standard volumes. A release that names a volume
// one edition doesn't have ("Vagabond Volume 022" for the 12-volume VizBig)
// is filed in the other edition that does, rather than failing.

import "github.com/rdborg/mediarium/internal/comics"

// comicIssueInOtherEdition finds issue `number` in another library series
// with the same name as sr, one not downloaded yet first.
func (s *Server) comicIssueInOtherEdition(sr comics.Series, number string) (comics.Series, comics.Issue, bool) {
	all, err := s.comicRepo().ListSeries()
	if err != nil {
		return comics.Series{}, comics.Issue{}, false
	}
	var foundSr comics.Series
	var found comics.Issue
	ok := false
	for _, other := range all {
		if other.ID == sr.ID || comics.Key(other.Name) != comics.Key(sr.Name) {
			continue
		}
		is, hit := s.comicIssueByNumber(other.ID, number)
		if !hit {
			continue
		}
		if !ok || (found.Status == comics.StatusDownloaded && is.Status != comics.StatusDownloaded) {
			foundSr, found, ok = other, is, true
		}
	}
	return foundSr, found, ok
}
