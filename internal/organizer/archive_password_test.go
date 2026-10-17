package organizer_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/organizer"
)

// The fixtures in testdata/encrypted are real archives made with the rar tool,
// all with the password "S3cret":
//
//	rar5-p.rar, rar4-p.rar     file data encrypted (rar a -p)
//	rar5-hp.rar, rar4-hp.rar   file data and file names encrypted (rar a -hp)
//	rar4-p-packed.rar          RAR4, compressed, file data encrypted
//	set5-hp.part1..3.rar       RAR5 volume set, names encrypted (rar a -hp -v12k)
//
// The single archives hold movie.mkv (fixtureMovie below); the set holds
// big.mkv, 30000 bytes, checked by its SHA-256.
const fixturePassword = "S3cret"

const bigMkvSHA256 = "4a338a45ce03b8a3bb90500982dd50c34074eaa3f0634c2e328840963b34523d"

func fixtureMovie() string {
	var b strings.Builder
	for i := 1; i <= 200; i++ {
		fmt.Fprintf(&b, "Mediarium test video line %d\n", i)
	}
	return b.String()
}

// copyFixtures copies the named fixtures into a new folder, so a test can
// unpack next to them without touching testdata.
func copyFixtures(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, n := range names {
		b, err := os.ReadFile(filepath.Join("testdata", "encrypted", n))
		if err != nil {
			t.Fatal(err)
		}
		writeTestFile(t, filepath.Join(dir, n), b)
	}
	return dir
}

func TestExtractRealEncryptedRAR(t *testing.T) {
	for _, name := range []string{"rar5-p.rar", "rar5-hp.rar", "rar4-p.rar", "rar4-hp.rar", "rar4-p-packed.rar"} {
		t.Run(name, func(t *testing.T) {
			dir := copyFixtures(t, name)
			archive := filepath.Join(dir, name)

			t.Run("right password", func(t *testing.T) {
				dest := filepath.Join(dir, "right")
				ex := &organizer.Extractor{Password: fixturePassword}
				if err := ex.Extract(archive, dest); err != nil {
					t.Fatalf("the right password must open it: %v", err)
				}
				if got := readTestFile(t, filepath.Join(dest, "movie.mkv")); got != fixtureMovie() {
					t.Errorf("movie.mkv has the wrong content (%d bytes)", len(got))
				}
			})

			t.Run("wrong password", func(t *testing.T) {
				dest := filepath.Join(dir, "wrong")
				ex := &organizer.Extractor{Password: "not-it"}
				err := ex.Extract(archive, dest)
				if !errors.Is(err, organizer.ErrWrongPassword) {
					t.Fatalf("err = %v, want ErrWrongPassword", err)
				}
				if !errors.Is(err, organizer.ErrPasswordProtected) {
					t.Errorf("a wrong password still counts as password protected: %v", err)
				}
				if got := entriesIn(t, dest); len(got) != 0 {
					t.Errorf("a wrong password must leave no file behind: %v", got)
				}
			})

			t.Run("no password", func(t *testing.T) {
				dest := filepath.Join(dir, "none")
				err := organizer.NewExtractor().Extract(archive, dest)
				if !errors.Is(err, organizer.ErrPasswordProtected) {
					t.Fatalf("err = %v, want ErrPasswordProtected", err)
				}
				if errors.Is(err, organizer.ErrWrongPassword) {
					t.Errorf("with no password at all it must not say the password is wrong: %v", err)
				}
				if got := entriesIn(t, dest); len(got) != 0 {
					t.Errorf("nothing is unpacked without the password: %v", got)
				}
			})
		})
	}
}

// A volume set with encrypted names opens from its first volume with the
// password, and a wrong one fails before anything is written.
func TestExtractRealEncryptedRARVolumeSet(t *testing.T) {
	dir := copyFixtures(t, "set5-hp.part1.rar", "set5-hp.part2.rar", "set5-hp.part3.rar")
	archive := filepath.Join(dir, "set5-hp.part1.rar")

	dest := filepath.Join(dir, "right")
	if err := (&organizer.Extractor{Password: fixturePassword}).Extract(archive, dest); err != nil {
		t.Fatalf("the right password must open the set: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dest, "big.mkv"))
	if err != nil {
		t.Fatal(err)
	}
	if sum := sha256.Sum256(b); hex.EncodeToString(sum[:]) != bigMkvSHA256 {
		t.Errorf("big.mkv has the wrong content (%d bytes)", len(b))
	}

	wrongDest := filepath.Join(dir, "wrong")
	if err := (&organizer.Extractor{Password: "not-it"}).Extract(archive, wrongDest); !errors.Is(err, organizer.ErrWrongPassword) {
		t.Fatalf("err = %v, want ErrWrongPassword", err)
	}
	if got := entriesIn(t, wrongDest); len(got) != 0 {
		t.Errorf("a wrong password must leave no file behind: %v", got)
	}
}
