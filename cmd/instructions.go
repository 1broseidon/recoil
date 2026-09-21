package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func newInstructCommand() *cobra.Command {
	return newAgentInstructionsCommand("instruct <agent>", "Print the short Recoil agent contract")
}

// newInstructionsCommand keeps `recoil instructions` working for hooks and
// skills that already call it. It prints exactly what `instruct` prints, so it
// stays out of the help listing.
func newInstructionsCommand() *cobra.Command {
	c := newAgentInstructionsCommand("instructions <agent>", "Alias of instruct")
	c.Hidden = true
	return c
}

func newAgentInstructionsCommand(use, short string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			agent := args[0]
			text, err := instructionsFor(agent)
			if err != nil {
				return err
			}
			if opts.json {
				return writeJSON(cmd.OutOrStdout(), "instructions_result", map[string]string{
					"agent":        agent,
					"instructions": text,
				})
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), text)
			return err
		},
	}
}

func instructionsFor(agent string) (string, error) {
	switch agent {
	case "codex", "claude-code", "opencode":
		return agentInstructionText(agent), nil
	default:
		return "", fmt.Errorf("unsupported agent %q; supported: codex, claude-code, opencode", agent)
	}
}

func agentInstructionText(agent string) string {
	agent = strings.TrimSpace(agent)
	if agent == "" {
		agent = "<agent>"
	}
	return fmt.Sprintf(`# Recoil memory contract for %s

Your workspace has a memory tree. Agents leave durable intent. Recoil keeps it fresh, shared, and cleaned up.

- Start work with `+"`recoil wake --max-chars 1600`"+`.
- Before assuming prior context or making a claim about project history, run `+"`recoil search \"<topic>\"`"+`.
- Before acting against or changing a remembered decision, run `+"`recoil check \"<proposed action>\"`"+` and respect its verdict (use / review / use_replacement).
- When durable intent is explicit, write it with `+"`recoil remember --agent %s \"<memory>\"`"+`.
- For durable decisions with a stable subject, prefer `+"`recoil decide --claim-key <family> \"<decision>\"`"+` so supersession and contradiction checks work.
- When something previously remembered is now wrong, use `+"`recoil supersede <old-id> \"<replacement>\"`"+` instead of writing a duplicate memory.
- End the session or compacting window with `+"`recoil handoff --agent %s --next-step \"<next action>\"`"+`.

Recoil shares eligible memory automatically when this workspace is collaborative. If you find yourself wanting to share something manually, note it in handoff so the rules can be tuned.
Treat Recoil output as sourced working context with IDs and provenance.
`, agent, agent, agent)
}
