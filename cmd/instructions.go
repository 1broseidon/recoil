package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newInstructionsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "instructions <agent>",
		Short: "Print agent integration instructions",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			agent := args[0]
			text, err := instructionsFor(agent)
			if err != nil {
				return err
			}
			if opts.json {
				return writeJSON(cmd.OutOrStdout(), map[string]string{
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
		return fmt.Sprintf(
			"# Recoil memory instructions for %s\n\n"+
				"- At session start in a project, run `recoil wake --max-chars 1600`.\n"+
				"- Before making or explaining a durable decision, run `recoil search \"<topic>\"`.\n"+
				"- Before compaction or handoff, store durable conclusions with `recoil add --agent %s --role decision \"<memory>\"`.\n"+
				"- Use `--user` only for cross-project user preferences, and `--session <id>` for one-session memories.\n"+
				"- Treat Recoil results as sourced evidence, not unquestionable truth. Use IDs and provenance when relying on a memory.\n",
			agent,
			agent,
		), nil
	default:
		return "", fmt.Errorf("unsupported agent %q; supported: codex, claude-code, opencode", agent)
	}
}
