package music

// IsVariousArtists reports whether an artist (or folder) name stands for
// compilations by many artists ("Various Artists", "VA"). Home fork: the
// music import skips these folders.
func IsVariousArtists(name string) bool { return variousArtists(name) }
