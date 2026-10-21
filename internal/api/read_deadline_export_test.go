package api

import (
	"net/http"
	"time"
)

// SetBodyTimesForTest shortens the body read limits and answers a function that puts them back.
func SetBodyTimesForTest(normal, long time.Duration) (restore func()) {
	oldN, oldL := bodyReadTime, longUploadTime
	bodyReadTime, longUploadTime = normal, long
	return func() { bodyReadTime, longUploadTime = oldN, oldL }
}

// WithBodyDeadlineForTest exposes the middleware to the tests.
func WithBodyDeadlineForTest(h http.Handler) http.Handler { return withBodyDeadline(h) }
