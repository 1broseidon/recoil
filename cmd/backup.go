package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type backupOptions struct {
	out string
	max int
}

type backupResult struct {
	Source    string   `json:"source"`
	Path      string   `json:"path"`
	SizeBytes int64    `json:"size_bytes"`
	Kept      []string `json:"kept"`
	Pruned    []string `json:"pruned,omitempty"`
}

func newBackupCommand() *cobra.Command {
	var bOpts backupOptions
	c := &cobra.Command{
		Use:   "backup",
		Short: "Create a consistent snapshot of the database",
		Long: `backup writes a self-contained SQLite snapshot of the current database
to a rotating set of files. The destination defaults to the same directory as
the active DB, but --out can target any path -- including a synced folder
(Syncthing, iCloud Drive, Dropbox) to participate in a distributed memory
surface across systems for the same user.

Each backup is itself a valid recoil database: restore by stopping any recoil
process and replacing --db-path with a backup file.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			st, source, err := openStore()
			if err != nil {
				return err
			}
			defer st.Close()

			settings, err := effectiveSettings()
			if err != nil {
				return err
			}
			dir := strings.TrimSpace(bOpts.out)
			if dir == "" {
				if v, _ := settings.Get("backup.dir"); strings.TrimSpace(v) != "" {
					dir = strings.TrimSpace(v)
				}
			}
			if dir == "" {
				dir = filepath.Dir(source)
			}
			max := bOpts.max
			if max <= 0 {
				max = settings.Int("backup.max", 3)
			}

			base := filepath.Base(source) + ".bak"
			timestamp := time.Now().UTC().Format("20060102T150405.000000000Z")
			dest := filepath.Join(dir, base+"."+timestamp)

			if err := st.Backup(dest); err != nil {
				return err
			}
			info, err := os.Stat(dest)
			if err != nil {
				return err
			}
			kept, pruned, err := rotateBackups(dir, base, max)
			if err != nil {
				return err
			}

			result := backupResult{
				Source:    source,
				Path:      dest,
				SizeBytes: info.Size(),
				Kept:      kept,
				Pruned:    pruned,
			}
			w := cmd.OutOrStdout()
			if opts.json {
				return writeJSON(w, "backup_result", result)
			}
			content := strings.Join(kept, "\n")
			return frontmatter(w, []kv{
				{k: "source", v: result.Source},
				{k: "path", v: result.Path},
				{k: "size_bytes", v: fmt.Sprintf("%d", result.SizeBytes)},
				{k: "kept_count", v: fmt.Sprintf("%d", len(kept))},
				{k: "pruned_count", v: fmt.Sprintf("%d", len(pruned))},
			}, content)
		},
	}
	c.Flags().StringVar(&bOpts.out, "out", "", "destination directory (default: backup.dir setting or same dir as DB)")
	c.Flags().IntVar(&bOpts.max, "max", 0, "maximum number of backups to keep (default: backup.max setting or 3)")
	return c
}

func rotateBackups(dir, base string, max int) (kept, pruned []string, err error) {
	if max <= 0 {
		max = 3
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, err
	}
	prefix := base + "."
	var all []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.HasPrefix(entry.Name(), prefix) {
			all = append(all, filepath.Join(dir, entry.Name()))
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(all)))
	if len(all) <= max {
		return all, nil, nil
	}
	kept = all[:max]
	pruned = all[max:]
	for _, p := range pruned {
		if err := os.Remove(p); err != nil {
			return nil, nil, err
		}
	}
	return kept, pruned, nil
}
