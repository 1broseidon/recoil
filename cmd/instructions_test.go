package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestInstructPrintsTheLifecycleContract(t *testing.T) {
	for _, agent := range []string{"codex", "claude-code", "opencode"} {
		c := newInstructCommand()
		var out bytes.Buffer
		c.SetOut(&out)
		c.SetErr(&bytes.Buffer{})
		c.SetArgs([]string{agent})
		if err := c.Execute(); err != nil {
			t.Fatal(err)
		}
		got := out.String()
		for _, want := range []string{
			"memory store",
			"recoil wake --max-chars 1600",
			"recoil search \"<topic>\"",
			"recoil check \"<proposed action>\"",
			"recoil remember --agent " + agent,
			"recoil decide --claim-key <family>",
			"recoil supersede <old-id>",
			"recoil handoff --agent " + agent + " --next-step",
			"IDs and provenance",
		} {
			if !strings.Contains(got, want) {
				t.Fatalf("expected %q in instruct output for %s:\n%s", want, agent, got)
			}
		}
		for _, forbidden := range []string{"memory tree", "shar", "channel", "relay", "recoil add"} {
			if strings.Contains(got, forbidden) {
				t.Fatalf("instruction text leaked old guidance %q:\n%s", forbidden, got)
			}
		}
	}

	alias := newInstructionsCommand()
	var aliasOut bytes.Buffer
	alias.SetOut(&aliasOut)
	alias.SetErr(&bytes.Buffer{})
	alias.SetArgs([]string{"codex"})
	if err := alias.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(aliasOut.String(), "recoil handoff --agent codex") {
		t.Fatalf("expected the instructions alias to print the contract:\n%s", aliasOut.String())
	}
}
