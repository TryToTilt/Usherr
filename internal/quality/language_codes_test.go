package quality_test

import (
	"reflect"
	"testing"

	"github.com/rdborg/mediarium/internal/quality"
)

func TestLanguageCodes(t *testing.T) {
	tests := []struct {
		name string
		want []string
	}{
		{"German", []string{"de"}},
		{" german ", []string{"de"}},
		{"Chinese", []string{"cn", "zh"}},
		{"Norwegian", []string{"nb", "no"}},
		{"English", []string{"en"}},
		{"Klingon", nil},
		{"Original", nil},
	}
	for _, tt := range tests {
		if got := quality.LanguageCodes(tt.name); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("LanguageCodes(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}
