package api

import (
	"github.com/rdborg/mediarium/internal/brand"
	"github.com/rdborg/mediarium/internal/updatecheck"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// Upstream's tests check upstream's behaviour: with Usherr's switches off.
func init() {
	usherrNoStats = false
	usherrNoPrivateReports = false
	brand.Reset()
	updatecheck.DefaultRepo = "rdborg/Mediarium"
	os.Setenv(adultEnv, "1") // the fork's scene tests switch the module on
	usherrAdultConfirm = false
}

// Usherr never sends private reports to Mediarium's developer, and never
// sends usage numbers.
func TestUsherrSendsNothingUpstream(t *testing.T) {
	usherrNoStats, usherrNoPrivateReports = true, true
	defer func() { usherrNoStats, usherrNoPrivateReports = false, false }()
	s := &Server{}
	if s.statsEnabled() {
		t.Fatal("usage numbers would be sent")
	}
	w := httptest.NewRecorder()
	s.handleSendFeedback(w, httptest.NewRequest(http.MethodPost, "/api/feedback", strings.NewReader(`{}`)))
	if w.Code != http.StatusGone {
		t.Fatalf("a private report wasn't refused: %d", w.Code)
	}
}

// The adult module stays off until the server allows it, and switching it on
// takes the typed phrase.
func TestUsherrAdultLock(t *testing.T) {
	old := os.Getenv(adultEnv)
	defer os.Setenv(adultEnv, old)
	defer func() { usherrAdultConfirm = false }()
	usherrAdultConfirm = true

	os.Unsetenv(adultEnv)
	if adultAllowed() {
		t.Fatal("allowed without the environment setting")
	}
	for _, v := range []string{"1", "true", "YES"} {
		os.Setenv(adultEnv, v)
		if !adultAllowed() {
			t.Fatalf("%q didn't allow it", v)
		}
	}
	os.Setenv(adultEnv, "0")
	if adultAllowed() {
		t.Fatal("0 allowed it")
	}
}

func TestUsherrAdultSwitchRefusals(t *testing.T) {
	old := os.Getenv(adultEnv)
	defer os.Setenv(adultEnv, old)
	defer func() { usherrAdultConfirm = false }()
	usherrAdultConfirm = true
	s := &Server{}
	put := func(body string) int {
		w := httptest.NewRecorder()
		s.handlePutSceneSettings(w, httptest.NewRequest(http.MethodPut, "/api/scenes/settings", strings.NewReader(body)))
		return w.Code
	}
	os.Unsetenv(adultEnv)
	if c := put(`{"enabled":true,"confirm":"I am 18 or older"}`); c != http.StatusForbidden {
		t.Fatalf("locked server: %d", c)
	}
	os.Setenv(adultEnv, "1")
	if c := put(`{"enabled":true}`); c != http.StatusBadRequest {
		t.Fatalf("no typed phrase: %d", c)
	}
	if c := put(`{"enabled":true,"confirm":"yes"}`); c != http.StatusBadRequest {
		t.Fatalf("the wrong phrase: %d", c)
	}
}
