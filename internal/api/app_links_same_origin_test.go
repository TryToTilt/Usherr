package api_test

import (
	"net/http"
	"testing"
)

// An add-on on Mediarium's own address cannot be shown in a frame (it would
// not be walled off from the signed-in session), but can open in a new tab.
func TestAppLinkOnOwnAddressCannotBeFramed(t *testing.T) {
	_, base, admin, _, _ := familyServer(t)
	own := base + "/tdarr/"
	putJSON(t, admin, base+"/api/apps", []map[string]any{{"name": "Tdarr", "url": own, "mode": "frame", "enabled": true}}, http.StatusBadRequest)
	putJSON(t, admin, base+"/api/apps", []map[string]any{{"name": "Tdarr", "url": own, "mode": "tab", "enabled": true}}, http.StatusOK)
}
