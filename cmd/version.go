package cmd

import (
	"fmt"
	"runtime/debug"

	"github.com/spf13/cobra"
)

// Build-time version info, set through
// -ldflags "-X github.com/1broseidon/recoil/cmd.version=..." by the Makefile
// and the release workflow. A `go install github.com/1broseidon/recoil@vX.Y.Z`
// build has none of that, so versionInfo falls back to the module build info
// and still reports the right tag.
var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

func versionInfo() (v, c, d string) {
	v, c, d = version, commit, date
	if v != "dev" && v != "" {
		return v, c, d
	}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return v, c, d
	}
	if bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		v = bi.Main.Version
	}
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			if c == "" || c == "unknown" {
				c = s.Value
			}
		case "vcs.time":
			if d == "" || d == "unknown" {
				d = s.Value
			}
		}
	}
	return v, c, d
}

func versionSummary() string {
	v, c, d := versionInfo()
	return fmt.Sprintf("%s (%s, %s)", v, c, d)
}

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show build version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			v, c, d := versionInfo()
			result := map[string]string{
				"version": v,
				"commit":  c,
				"date":    d,
			}
			if opts.json {
				return writeJSON(cmd.OutOrStdout(), "version_result", result)
			}
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "recoil %s\n", versionSummary())
			return err
		},
	}
}
