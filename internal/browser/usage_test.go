package browser

import "testing"

func TestProcUsageFooterRoundsMemory(t *testing.T) {
	u := procUsage{rss: 18<<20 + 256<<10}
	if got, want := u.footer(), "MEM 18 MB"; got != want {
		t.Fatalf("footer = %q, want %q", got, want)
	}
	u.rss = 18<<20 + 600<<10
	if got, want := u.footer(), "MEM 19 MB"; got != want {
		t.Fatalf("footer = %q, want %q", got, want)
	}
}
