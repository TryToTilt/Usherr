package auth

import "testing"

func TestAudiobooksFollowOldBooksSwitch(t *testing.T) {
	if p := parsePermissions(`{"books":false}`); p.Audiobooks || p.Books {
		t.Fatalf("an account without books keeps no audiobooks: %+v", p)
	}
	if p := parsePermissions(`{"books":true}`); !p.Audiobooks {
		t.Fatalf("an account with books keeps audiobooks: %+v", p)
	}
	if p := parsePermissions(`{"books":false,"audiobooks":true}`); !p.Audiobooks || p.Books {
		t.Fatalf("set apart, they stay apart: %+v", p)
	}
	if p := parsePermissions(""); p.NSFW || !p.Comics {
		t.Fatalf("defaults: NSFW off, comics on: %+v", p)
	}
}
