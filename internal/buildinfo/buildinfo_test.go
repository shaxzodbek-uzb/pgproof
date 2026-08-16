package buildinfo

import "testing"

func TestStringRendersAllThreeFields(t *testing.T) {
	old := [3]string{Version, Commit, Date}
	oldFlag := dateIsCommitTime
	defer func() {
		Version, Commit, Date = old[0], old[1], old[2]
		dateIsCommitTime = oldFlag
	}()

	Version, Commit, Date = "v1.2.3", "abc1234", "2026-08-16T00:00:00Z"

	dateIsCommitTime = false
	want := "pgproof v1.2.3 (commit abc1234, built 2026-08-16T00:00:00Z)"
	if got := String(); got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}

	// A date recovered from VCS metadata is the commit time, not a build time.
	// Saying "built" there would be a claim the binary cannot support.
	dateIsCommitTime = true
	want = "pgproof v1.2.3 (commit abc1234, commit dated 2026-08-16T00:00:00Z)"
	if got := String(); got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

// `go test` builds a test binary from the working tree, so the module version is
// "(devel)" and there is no release tag to recover. The point here is that the
// fallback leaves the placeholders alone rather than writing "(devel)" into the
// version banner — a wrong-looking version is worse than an honest "dev".
func TestFallbackNeverReportsDevel(t *testing.T) {
	if Version == "(devel)" {
		t.Error(`Version is "(devel)"; the fallback must skip that value`)
	}
	if Commit != "none" && len(Commit) != 7 {
		t.Errorf("Commit = %q; a recovered VCS revision must be shortened to 7 chars", Commit)
	}
}
