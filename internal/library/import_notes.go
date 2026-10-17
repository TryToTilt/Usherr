package library

import "fmt"

// leftOutNote is the import report's note for a show whose files named
// episodes the movie database doesn't list: how many, and what to do about
// it when the names say. byAbsolute is true when some of those files were
// named by their number counted from the start of the show, byNumber when
// some had a season and episode. Each case is one whole sentence, so the
// screen can show it in the reader's language.
func leftOutNote(unknown int, byAbsolute, byNumber, anime bool) string {
	if unknown <= 0 {
		return ""
	}
	setAnime := byAbsolute && !anime
	rename := !setAnime && (byAbsolute || (byNumber && anime))
	switch {
	case unknown == 1 && setAnime:
		return "Left out 1 episode that the movie database doesn't list for this show. Its name carries the episode number counted from the start of the show, so set this show's Episode numbering to Anime and import the folder again."
	case setAnime:
		return fmt.Sprintf("Left out %d episodes that the movie database doesn't list for this show. Their names carry the episode number counted from the start of the show, so set this show's Episode numbering to Anime and import the folder again.", unknown)
	case unknown == 1 && rename:
		return "Left out 1 episode that the movie database doesn't list for this show. Its number doesn't match the movie database's list, so nothing was guessed. Rename the file to the season and episode shown on the show's page and import the folder again."
	case rename:
		return fmt.Sprintf("Left out %d episodes that the movie database doesn't list for this show. Their numbers don't match the movie database's list, so nothing was guessed. Rename the files to the season and episode shown on the show's page and import the folder again.", unknown)
	case unknown == 1:
		return "Left out 1 episode that the movie database doesn't list for this show."
	}
	return fmt.Sprintf("Left out %d episodes that the movie database doesn't list for this show.", unknown)
}
