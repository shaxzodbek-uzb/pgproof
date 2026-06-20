package verify

import "testing"

func TestQuotePgIdent(t *testing.T) {
	cases := map[string]string{
		"users":        `"users"`,
		"public.users": `"public"."users"`,
		"MixedCase":    `"MixedCase"`,
		`we"ird`:       `"we""ird"`, // defence in depth: embedded quote is doubled
		"a.b.c":        `"a"."b.c"`, // only the first dot splits schema.table
	}
	for in, want := range cases {
		if got := quotePgIdent(in); got != want {
			t.Errorf("quotePgIdent(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestQuoteMyIdent(t *testing.T) {
	cases := map[string]string{
		"users":     "`users`",
		"app.users": "`app`.`users`",
		"we`ird":    "`we``ird`",
	}
	for in, want := range cases {
		if got := quoteMyIdent(in); got != want {
			t.Errorf("quoteMyIdent(%q) = %q, want %q", in, got, want)
		}
	}
}
