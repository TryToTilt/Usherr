package api_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/rdborg/mediarium/internal/library"
)

// "Only take releases in these languages" is off until it is turned on, is
// kept when other settings are saved, and shows in the settings answer.
func TestLanguageOnlySetting(t *testing.T) {
	_, base, client := loginNewServer(t)
	saved := func() any { return getJSON[map[string]any](t, client, base+"/api/settings")["qualityLanguageOnly"] }

	if got := saved(); got != false {
		t.Fatalf("the switch should start off, got %v", got)
	}
	putJSONStatus(t, client, base+"/api/settings", map[string]any{"qualityLanguageOnly": true}, http.StatusOK)
	if got := saved(); got != true {
		t.Fatalf("saved %v after turning it on", got)
	}
	putJSONStatus(t, client, base+"/api/settings", map[string]any{"qualityLanguage": "German"}, http.StatusOK)
	if got := saved(); got != true {
		t.Fatalf("saving the languages changed the switch to %v", got)
	}
	putJSONStatus(t, client, base+"/api/settings", map[string]any{"qualityLanguageOnly": false}, http.StatusOK)
	if got := saved(); got != false {
		t.Fatalf("saved %v after turning it off", got)
	}
}

// With the switch on, automatic searches and the RSS sync leave out releases
// that do not say they are in a wanted language, and the release list says why
// while a manual grab still works. With it off nothing changes.
func TestAutomationOnlyTakesTheWantedLanguages(t *testing.T) {
	const (
		plain     = "Fixture.Show.S01E01.1080p.WEB-DL.x264-GRP"
		english   = "Fixture.Show.S01E01.ENG.1080p.WEB-DL.x264-GRP"
		bareMulti = "Fixture.Show.S01E01.MULTi.1080p.WEB-DL.x264-GRP"
		multiDeEn = "Fixture.Show.S01E01.MULTi.GERMAN.ENG.1080p.WEB-DL.x264-GRP"
		german    = "Fixture.Show.S01E01.GERMAN.1080p.WEB-DL.x264-GRP"
	)
	missing := []episodeSpec{{1, 1, "2020-01-01", library.StatusMissing, ""}}
	tests := []struct {
		name     string
		language string
		only     bool
		titles   []string
		want     []string
	}{
		{"off: untagged is still taken when German is wanted", "German", false, []string{plain}, []string{plain}},
		{"on: untagged is left out", "German", true, []string{plain, english, bareMulti}, nil},
		{"on: German is taken when the rest do not say German", "German", true, []string{plain, german}, []string{german}},
		{"on: a multi release naming German is taken", "German", true, []string{plain, multiDeEn}, []string{multiDeEn}},
		{"on: German then English takes untagged as English", "German,English", true, []string{plain}, []string{plain}},
		{"on: English wanted keeps untagged", "English", true, []string{plain, german}, []string{plain}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env := newTVAutoEnv(t, tc.titles, missing)
			putJSONStatus(t, env.client, env.baseURL+"/api/settings", map[string]any{"qualityLanguage": tc.language, "qualityLanguageOnly": tc.only}, http.StatusOK)
			env.server.TestHunt(context.Background())
			requireTitles(t, env.grabbedTitles(t), tc.want...)
			env.finish(t)
		})
	}

	t.Run("the RSS sync applies the same rule", func(t *testing.T) {
		env := newTVAutoEnv(t, []string{plain, german}, missing)
		putJSONStatus(t, env.client, env.baseURL+"/api/settings", map[string]any{"qualityLanguage": "German", "qualityLanguageOnly": true}, http.StatusOK)
		env.server.TestRSSSync(context.Background())
		requireTitles(t, env.grabbedTitles(t), german)
		env.finish(t)
	})

	t.Run("the release list says why and a manual grab still works", func(t *testing.T) {
		env := newTVAutoEnv(t, []string{plain, german}, missing)
		putJSONStatus(t, env.client, env.baseURL+"/api/settings", map[string]any{"qualityLanguage": "German", "qualityLanguageOnly": true}, http.StatusOK)
		type row struct {
			Title       string   `json:"title"`
			Language    string   `json:"language"`
			LanguageFit string   `json:"languageFit"`
			Rejections  []string `json:"rejections"`
			DownloadURL string   `json:"downloadUrl"`
		}
		byTitle := map[string]row{}
		for _, r := range getJSON[[]row](t, env.client, env.baseURL+"/api/series/"+itoa(env.seriesID)+"/search?season=1&episode=1") {
			byTitle[r.Title] = r
		}
		if r := byTitle[plain]; r.LanguageFit != "other" || r.Language != "Not stated" ||
			len(r.Rejections) == 0 || r.Rejections[0] != "Untagged release, taken to be English, not German" {
			t.Errorf("the untagged release should be marked and explained: %+v", r)
		}
		if r := byTitle[german]; r.LanguageFit != "ok" || len(r.Rejections) != 0 {
			t.Errorf("the German release is fine: %+v", r)
		}
		postJSON[map[string]any](t, env.client, env.baseURL+"/api/series/"+itoa(env.seriesID)+"/grab", map[string]any{
			"releaseTitle": plain, "downloadUrl": byTitle[plain].DownloadURL, "protocol": "usenet", "season": 1, "episode": 1,
		}, http.StatusAccepted)
		env.finish(t)
	})
}
