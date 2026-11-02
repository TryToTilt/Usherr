package api

// homeForkBuild marks this as the home fork: new upstream versions are
// merged into it by hand, so Mediarium never installs an upstream release
// over it by itself (that would drop every home feature).
var homeForkBuild = true

// Usherr: shown to everyone running Usherr, from its Docker image or built
// from its source, so it says how to update either way.
const homeForkUpdateNote = "Update now is switched off in Usherr. To update, pull the new image and restart it (docker compose pull, then docker compose up -d), or, if you build Usherr yourself, build the new release."
