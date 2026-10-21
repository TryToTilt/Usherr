package api_test

import (
	"net/http"
	"testing"
)

// The household picks where Tickets goes: one of the services, a link of its
// own, or none. Anything else is turned away.
func TestCinemaTicketService(t *testing.T) {
	_, base, admin := loginNewServer(t)
	put := func(body map[string]any) int {
		st, _ := doJSONStatus(t, admin, http.MethodPut, base+"/api/home/cinema/settings", body)
		return st
	}
	if put(map[string]any{"tickets": "scalpers"}) != http.StatusBadRequest {
		t.Fatal("an unknown service was kept")
	}
	for _, bad := range []string{"javascript:alert(1)", "https://tix.example.com/{title}\" onclick=\"x", "ftp://tix.example.com"} {
		if put(map[string]any{"tickets": "custom", "ticketsUrl": bad}) != http.StatusBadRequest {
			t.Fatalf("%q was kept", bad)
		}
	}
	if put(map[string]any{"tickets": "custom", "ticketsUrl": "https://tix.example.com/find?film={title}&zip={zip}"}) != http.StatusOK {
		t.Fatal("a good link was turned away")
	}
	got := getJSON[map[string]any](t, admin, base+"/api/home/cinema/settings")
	if got["tickets"] != "custom" || got["ticketsUrl"] != "https://tix.example.com/find?film={title}&zip={zip}" || len(got["logos"].(map[string]any)) != 0 {
		t.Fatalf("settings: %v", got)
	}
	if put(map[string]any{"tickets": "atom"}) != http.StatusOK || getJSON[map[string]any](t, admin, base+"/api/home/cinema")["tickets"] != "atom" {
		t.Fatal("Atom Tickets not chosen")
	}
}
