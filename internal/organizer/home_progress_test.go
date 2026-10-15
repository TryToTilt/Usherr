package organizer

import (
	"strings"
	"testing"
)

func TestPar2WatchReportsCheckingAndRepair(t *testing.T) {
	type rep struct {
		phase       string
		done, total int64
	}
	var got []rep
	ProgressHook = func(path, phase string, done, total int64) { got = append(got, rep{phase, done, total}) }
	defer func() { ProgressHook = nil }()

	w := newPar2Watch("/data/downloads/incomplete/queue-7/x.par2")
	out := "Loading \"x.par2\".\nThere are 4 recoverable files and 0 other files.\n" +
		"Scanning: \"x.part1.rar\": 50.0%\rTarget: \"x.part1.rar\" - found.\n" +
		"Target: \"x.part2.rar\" - damaged.\n" +
		"Repairing: 10.0%\rRepairing: 100.0%\n" +
		"Verifying repaired files:\nTarget: \"x.part2.rar\" - found.\n"
	if _, err := w.Write([]byte(out)); err != nil {
		t.Fatal(err)
	}
	if len(got) < 3 {
		t.Fatalf("reports = %+v", got)
	}
	if got[0].phase != "Checking files" || got[0].done != 500 || got[0].total != 4000 {
		t.Errorf("first report = %+v, want checking 500/4000", got[0])
	}
	var phases []string
	for _, r := range got {
		if r.phase != "" {
			phases = append(phases, r.phase)
		}
	}
	if j := strings.Join(phases, ","); j != "Checking files,Repairing damaged files,Checking the repaired files" {
		t.Errorf("phases = %s", j)
	}
	if !strings.Contains(string(w.Bytes()), "There are 4 recoverable files") {
		t.Error("output not kept for error messages")
	}
}
