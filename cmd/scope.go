package cmd

import (
	"fmt"
	"strings"

	"github.com/1broseidon/recoil/internal/scope"
	"github.com/spf13/cobra"
)

type scopeOptions struct {
	user    bool
	project string
	session string
}

func addScopeFlags(c *cobra.Command, opts *scopeOptions) {
	c.Flags().BoolVar(&opts.user, "user", false, "use the persistent user scope")
	c.Flags().StringVar(&opts.project, "project", "", "use project scope for the given workspace path")
	c.Flags().StringVar(&opts.session, "session", "", "use session scope for the given session ID")
}

func resolveScope(cmd *cobra.Command, o scopeOptions) (scope.Scope, error) {
	return resolveScopeWithDefault(cmd, o, "project")
}

func resolveReadScope(cmd *cobra.Command, o scopeOptions) (scope.Scope, error) {
	return resolveScopeWithDefault(cmd, o, "project")
}

func resolveWakeScope(cmd *cobra.Command, o scopeOptions) (scope.Scope, error) {
	return resolveScopeWithDefault(cmd, o, "project")
}

func resolveScopeWithDefault(cmd *cobra.Command, o scopeOptions, defaultKind string) (scope.Scope, error) {
	selected := 0
	if o.user {
		selected++
	}
	if o.project != "" {
		selected++
	}
	if o.session != "" {
		selected++
	}
	if selected > 1 {
		return scope.Scope{}, fmt.Errorf("choose only one of --user, --project, or --session")
	}
	if o.user {
		return scope.UserScope()
	}
	if o.project != "" {
		sc, err := scope.ProjectScope(o.project)
		if err == nil {
			warnUninitializedProject(cmd, sc, true)
		}
		return sc, err
	}
	if o.session != "" {
		return scope.SessionScope(o.session)
	}
	if defaultKind == "project" {
		sc, err := scope.ProjectScope(".")
		if err == nil {
			warnUninitializedProject(cmd, sc, false)
		}
		return sc, err
	}
	return scope.ProjectScope(".")
}

func warnUninitializedProject(cmd *cobra.Command, sc scope.Scope, explicit bool) {
	if cmd == nil || sc.Kind != "project" || sc.Initialized {
		return
	}
	var b strings.Builder
	if explicit {
		fmt.Fprintf(&b, "warning: project path %q is not initialized for Recoil", sc.Root)
	} else {
		fmt.Fprintf(&b, "warning: current directory is not inside an initialized Recoil project")
	}
	fmt.Fprintf(&b, "; run `recoil init` or pass --user/--session")
	if explicit {
		fmt.Fprintf(&b, " for a non-project memory")
	}
	fmt.Fprintln(cmd.ErrOrStderr(), b.String())
}
