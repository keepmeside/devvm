package cli

import (
	"testing"
)

func TestExecLineQuotesAndRunsAsRoot(t *testing.T) {
	got := execLine([]string{"sh", "-c", "mkdir -p /opt/stella && tar -xf - -C /opt/stella"}, true)
	want := "'sudo' '-n' '-H' '--' 'sh' '-c' 'mkdir -p /opt/stella && tar -xf - -C /opt/stella'"
	if got != want {
		t.Fatalf("execLine = %q, want %q", got, want)
	}
	if got := execLine([]string{"cat", "/root/it's"}, false); got != `'cat' '/root/it'\''s'` {
		t.Fatalf("execLine = %q", got)
	}
}
