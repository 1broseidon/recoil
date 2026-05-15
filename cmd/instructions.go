package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func newInstructCommand() *cobra.Command {
	return newAgentInstructionsCommand("instruct <agent>", "Print the short Recoil agent contract")
}

func newInstructionsCommand() *cobra.Command {
	return newAgentInstructionsCommand("instructions <agent>", "Print agent integration instructions")
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
- When durable intent is explicit, write it with `+"`recoil remember --agent %s \"<memory>\"`"+`.
- End the session or compacting window with `+"`recoil handoff --agent %s \"<summary>\"`"+`.

Recoil shares eligible memory automatically when this workspace is collaborative. If you find yourself wanting to share something manually, note it in handoff so the rules can be tuned.

Treat Recoil output as sourced working context with IDs and provenance.
`, agent, agent, agent)
}
