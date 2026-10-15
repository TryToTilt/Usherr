// Package brand is this fork's name. Usherr is built on Mediarium by rdborg
// (AGPL-3.0); upstream's code says "Mediarium", and the places a person sees
// the name outside the web page (notifications, emails, the authenticator
// app, passkeys, the e-book feed) use Name instead.
package brand

import "regexp"

// Name is the app's name. A variable so upstream's tests, which expect
// "Mediarium", can set it back (their packages' usherr_test.go).
var Name = "Usherr"

// Upstream is the project Usherr is built on.
const Upstream = "Mediarium"

// URL is the project's page, given to other services with the name. A
// variable, like Name, so upstream's tests can set it back.
var URL = "https://github.com/TryToTilt/Usherr"

// UpstreamURL is Mediarium's page.
const UpstreamURL = "https://github.com/rdborg/Mediarium"

// Agent is how the app introduces itself to another service: "Usherr/1.0.0
// ( https://github.com/TryToTilt/Usherr )".
func Agent(version string) string { return Name + "/" + version + " ( " + URL + " )" }

// Reset sets the name and page back to upstream's, for upstream's tests.
func Reset() { Name, URL = Upstream, UpstreamURL }

var word = regexp.MustCompile(`(^|[^\w/.@-])Mediarium($|[^\w/-])`)

// Text shows "Mediarium" as a word as Name; addresses keep it.
func Text(s string) string {
	if Name == Upstream {
		return s
	}
	for i := 0; i < 3 && word.MatchString(s); i++ { // overlapping matches ("Mediarium Mediarium")
		s = word.ReplaceAllString(s, "${1}"+Name+"${2}")
	}
	return s
}
