package brand

import "testing"

func TestText(t *testing.T) {
	for in, want := range map[string]string{
		"Mediarium: Dune is ready":                     "Usherr: Dune is ready",
		"Mediarium Mediarium":                          "Usherr Usherr",
		"see github.com/rdborg/Mediarium for more":     "see github.com/rdborg/Mediarium for more",
		"mediarium.app and MEDIARIUM_PAUSE_AUTOMATION": "mediarium.app and MEDIARIUM_PAUSE_AUTOMATION",
		"Mediarium's own folder":                       "Usherr's own folder",
	} {
		if got := Text(in); got != want {
			t.Errorf("Text(%q) = %q, want %q", in, got, want)
		}
	}
}
