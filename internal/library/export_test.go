package library

// What the tests in library_test need to see from inside the package.

var (
	WantedShowsQuery        = wantedShowsQuery
	WantedEpisodeFilesQuery = wantedEpisodeFilesQuery
	WantedEpisodesQuery     = wantedEpisodesQuery
)

const EpisodeFilesQuery = episodeFilesQuery

// SetAfterWatchGroup sets the hook StartWatchingImport runs after each group
// of titles it has saved.
func SetAfterWatchGroup(fn func()) { afterWatchGroup = fn }
