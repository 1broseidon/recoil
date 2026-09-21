package cmd

import (
	"fmt"
	"os"
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

// ProjectEnvVar aims project scope without a flag, for agents and hooks that
// cannot control their working directory. --project still wins; this only fills
// in for cwd inference.
const ProjectEnvVar = "RECOIL_PROJECT"

// projectFromEnv returns the RECOIL_PROJECT aim, if any.
func projectFromEnv() string {
	return strings.TrimSpace(os.Getenv(ProjectEnvVar))
}

// envAimedProjectScope resolves project scope for the internal paths that take
// no scope flags (hooks, config, the project-local DB fallback, swarm, status).
// Without this they would keep inferring from the working directory and disagree
// with the flag-bearing commands about which project is in play.
func envAimedProjectScope() (scope.Scope, error) {
	if envProject := projectFromEnv(); envProject != "" {
		return scope.ProjectScope(envProject)
	}
	return scope.ProjectScope(".")
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
	// Precedence: an explicit flag above, then RECOIL_PROJECT, then cwd.
	// Reaching here means no scope flag was given, so the env aim applies
	// exactly as if --project had been passed.
	if envProject := projectFromEnv(); envProject != "" {
		sc, err := scope.ProjectScope(envProject)
		if err == nil {
			warnUninitializedProject(cmd, sc, true)
		}
		return sc, err
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
	// Uninitialized but inside a git repository still yields a stable, shareable
	// scope. Uninitialized AND outside any repository does not: the id is just a
	// hash of this path, so memories written here are unreachable from anywhere
	// else. Say so on the same line, and say how to aim.
	if sc.Unknown {
		fmt.Fprintf(&b, "; no git repository here either, so scope %s is addressable only from this exact path -- aim it with --project <path> or %s",
			sc.ID, ProjectEnvVar)
	}
	_, _ = fmt.Fprintln(cmd.ErrOrStderr(), b.String())
}
