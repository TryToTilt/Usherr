//go:build race

package indexers

// Usherr: the race detector makes code several times slower, so the timing
// checks allow for it there (the ordinary test run keeps the real limits).
const raceSlowdown = 10
