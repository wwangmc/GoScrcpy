package mobileshell

import (
	"strings"
	"testing"
)

func TestShellResultRequiresSuccessfulExit(t *testing.T) {
	output, err := ShellOutput("installed\n" + exitMarker + "0\n")
	if err != nil || output != "installed" {
		t.Fatalf("output=%q error=%v", output, err)
	}
	for _, output := range []string{"installed", "\n" + exitMarker + "bad", "\n" + exitMarker + "1", "\n" + exitMarker + "127"} {
		if _, err := ShellOutput(output); err == nil {
			t.Fatalf("accepted incomplete or failed result %q", output)
		}
	}
}

func TestShellQuoteAndCommand(t *testing.T) {
	if quoted := ShellQuote("a'b"); quoted != `'a'"'"'b'` {
		t.Fatalf("unsafe quoting: %s", quoted)
	}
	command := ShellCommand("false")
	if !strings.Contains(command, "false 2>&1; gs_shell_exit=$?") || !strings.Contains(command, exitMarker) {
		t.Fatal("command did not capture exit status", command)
	}
}
