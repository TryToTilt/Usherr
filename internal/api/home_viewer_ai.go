package api

// Home fork: a look-only (viewer) account may still ask the AI. These calls
// are POSTs only because a question can be long; they answer and change
// nothing. Making a playlist (it saves one) and the AI settings test stay
// out. The daily AI limit still applies.
var viewerAsks = map[string]bool{
	"POST /api/home/ai/ask":             true,
	"POST /api/home/ai/search":          true,
	"POST /api/home/ai/room":            true,
	"POST /api/home/ai/recap":           true,
	"POST /api/home/ai/tonight":         true,
	"POST /api/home/ai/explain":         true,
	"POST /api/home/ai/comic-recap":     true,
	"POST /api/home/ai/similar-artists": true,
	"POST /api/home/ai/book-search":     true,
	"POST /api/home/ai/read-alikes":     true,
	"POST /api/home/ai/book-recap":      true,
	"POST /api/home/ai/vibe":            true,
	"POST /api/home/ai/skip-guide":      true,
	"POST /api/home/ai/comic-guide":     true,
}

// viewerOwn (home fork): a viewer's own Discover recipes and streaming
// services; they belong to the person, like their profile.
var viewerOwn = map[string]bool{
	"POST /api/home/recipes":           true,
	"PUT /api/home/recipes/{id}":       true,
	"DELETE /api/home/recipes/{id}":    true,
	"POST /api/home/recipes/{id}/copy": true,
	"PUT /api/home/my-services":        true,
	"POST /api/home/gaps/ignore":       true,
}
