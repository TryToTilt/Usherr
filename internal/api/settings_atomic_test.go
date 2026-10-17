package api_test

import (
	"net/http"
	"testing"
)

// A request with one bad value saves nothing: not the folders or the naming
// style that come before it, whichever check turns it down.
func TestPutSettingsSavesNothingWhenALaterValueIsRefused(t *testing.T) {
	_, base, client := newServerWithDB(t)
	extra := t.TempDir()

	for name, bad := range map[string]map[string]any{
		"subtitle language code": {"subtitleProfiles": map[string]any{"not a code!": map[string]any{}}},
		"speed limit":            {"speedLimitMb": 999999},
		"backups to keep":        {"backupKeep": 0},
		"quiet hours":            {"notifyQuietHours": "late"},
	} {
		t.Run(name, func(t *testing.T) {
			body := map[string]any{"moviesExtraPaths": []string{extra}, "namingPreset": "kodi"}
			for k, v := range bad {
				body[k] = v
			}
			putJSONStatus(t, client, base+"/api/settings", body, http.StatusBadRequest)

			got := getJSON[map[string]any](t, client, base+"/api/settings")
			if list, _ := got["moviesExtraPaths"].([]any); len(list) != 0 {
				t.Errorf("the extra folder was saved although the request was refused: %v", list)
			}
			if got["namingPreset"] == "kodi" {
				t.Errorf("the naming style was saved although the request was refused")
			}
		})
	}
}
