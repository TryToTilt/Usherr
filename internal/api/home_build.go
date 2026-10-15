package api

// homeForkBuild marks this as the home fork: new upstream versions are
// merged into it by hand, so Mediarium never installs an upstream release
// over it by itself (that would drop every home feature).
var homeForkBuild = true

const homeForkUpdateNote = "This is your home build of Mediarium. New versions are merged into it by hand, so Update now is switched off here: installing the plain release would remove your home features."
