package api

import "database/sql"

// TestSetKoboStore points the Kobo store passthrough at a fake server.
func (s *Server) TestSetKoboStore(baseURL string) {
	rt := s.ereaderState()
	rt.storeMu.Lock()
	rt.storeBase = baseURL
	rt.storeMu.Unlock()
}

// TestDB is the server's database, for tests that change rows directly.
func (s *Server) TestDB() *sql.DB { return s.db }

// TestEReaderBuilds reports how many times the ebook list was read from the
// database and how many catalogue pages were built since the server started.
func (s *Server) TestEReaderBuilds() (lists, feeds int64) {
	rt := s.ereaderState()
	return rt.listBuilds.Load(), rt.feedBuilds.Load()
}

// LimitKepubEntryForTest lowers the size one file of an EPUB may unpack to
// before it is not converted, and returns the function that puts it back.
func LimitKepubEntryForTest(bytes uint64) (restore func()) {
	old := kepubZipLimits
	kepubZipLimits.entry = bytes
	return func() { kepubZipLimits = old }
}
