package api

// TestSearchSeriesNow runs the search that follows adding a show, changing its
// profile or blocklisting one of its releases.
func (s *Server) TestSearchSeriesNow(seriesID int64) { s.searchSeriesInBackground(seriesID) }

// TestSetSearchTime dates the "searched" notes of a show.
func (s *Server) TestSetSearchTime(seriesID int64, at string) error {
	_, err := s.db.Exec(`UPDATE activity SET created_at = ? WHERE series_id = ? AND event_type = 'searched'`, at, seriesID)
	return err
}

// TestSearchOrder gives the order a hunt takes these shows in.
func (s *Server) TestSearchOrder(seriesIDs []int64) []int64 {
	wants := make([]tvWant, len(seriesIDs))
	for i, id := range seriesIDs {
		wants[i].series.ID = id
	}
	s.leastRecentlySearchedFirst(wants, nil)
	out := make([]int64, len(wants))
	for i, w := range wants {
		out[i] = w.series.ID
	}
	return out
}
