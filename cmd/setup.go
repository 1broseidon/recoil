package cmd

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/1broseidon/recoil/internal/config"
	"github.com/1broseidon/recoil/internal/scope"
	"github.com/spf13/cobra"
)

type setupOptions struct {
	agents        []string
	noHooks       bool
	hookScope     string
	relay         string
	relayAgent    string
	standalone    bool
	collaborative bool
	manualShare   bool
}

const (
	setupPostureStandalone    = "standalone"
	setupPostureCollaborative = "collaborative"
	setupPostureManualShare   = "manual-share"
)

type setupHookResult struct {
	Agent  string `json:"agent"`
	Target string `json:"target,omitempty"`
	Error  string `json:"error,omitempty"`
}

type setupResult struct {
	Init        initResult         `json:"init"`
	Mine        mineResult         `json:"mine"`
	Detected    []string           `json:"detected_agents"`
	Hooks       []setupHookResult  `json:"hooks"`
	Channel     *channelJoinResult `json:"channel,omitempty"`
	Posture     string             `json:"posture,omitempty"`
	NextCommand string             `json:"next_command"`
}

func newSetupCommand() *cobra.Command {
	var setupOpts setupOptions
	c := &cobra.Command{
		Use:   "setup",
		Short: "Bootstrap Recoil for this project in one step",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			posture, err := setupPosture(setupOpts)
			if err != nil {
				return err
			}
			initResult, sc, err := runInitProject(ctx)
			if err != nil {
				return err
			}
			if posture != "" {
				if err := writeSetupPostureConfig(sc, posture); err != nil {
					return err
				}
			}
			st, _, err := openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			mineResult, err := runMineFiles(ctx, st, sc, sc.Root, mineOptions{
				role:         "source",
				agent:        "recoil",
				maxFileBytes: mineDefaultMaxFileBytes(),
				maxChars:     mineDefaultMaxChunkChars(),
			})
			if err != nil {
				return err
			}
			agents := normalizedSetupAgents(setupOpts.agents)
			if len(agents) == 0 {
				agents = detectInstalledAgents()
			}
			hooks := make([]setupHookResult, 0, len(agents))
			if !setupOpts.noHooks {
				for _, agent := range agents {
					adapter, err := lookupHookAdapter(agent)
					if err != nil {
						hooks = append(hooks, setupHookResult{Agent: agent, Error: err.Error()})
						continue
					}
					target, _, err := adapter.install(firstNonEmpty(setupOpts.hookScope, "project"), false)
					item := setupHookResult{Agent: agent, Target: target}
					if err != nil {
						item.Error = err.Error()
					}
					hooks = append(hooks, item)
				}
			}
			var channelResult *channelJoinResult
			if strings.TrimSpace(setupOpts.relay) != "" {
				identity, err := st.GetOrCreateChannelIdentity(ctx)
				if err != nil {
					return err
				}
				agent := firstNonEmpty(setupOpts.relayAgent, firstSetupAgent(agents), "recoil")
				joined, err := joinRelayChannel(ctx, st, identity, sc, agent, setupOpts.relay)
				if err != nil {
					return err
				}
				channelResult = &joined
			}
			result := setupResult{
				Init:        initResult,
				Mine:        mineResult,
				Detected:    agents,
				Hooks:       hooks,
				Channel:     channelResult,
				Posture:     setupDisplayPosture(posture, channelResult),
				NextCommand: "recoil wake",
			}
			if opts.json {
				return writeJSON(cmd.OutOrStdout(), "setup_result", result)
			}
			var body strings.Builder
			fmt.Fprintf(&body, "memory tree: project\n")
			fmt.Fprintf(&body, "posture: %s\n", result.Posture)
			fmt.Fprintf(&body, "sharing: %s\n", setupSharingState(channelResult))
			fmt.Fprintf(&body, "evidence: %s\n\n", setupEvidenceState(result.Posture))
			fmt.Fprintf(&body, "indexed %d chunks from %d files\n", mineResult.Chunks, mineResult.FilesScanned)
			if len(agents) > 0 {
				fmt.Fprintf(&body, "detected agents: %s\n", strings.Join(agents, ", "))
			} else {
				body.WriteString("detected agents: none\n")
			}
			for _, hook := range hooks {
				if hook.Error != "" {
					fmt.Fprintf(&body, "hook %s: %s\n", hook.Agent, hook.Error)
				} else {
					fmt.Fprintf(&body, "hook %s: %s\n", hook.Agent, hook.Target)
				}
			}
			if channelResult != nil {
				fmt.Fprintf(&body, "sharing peer: %s\n", channelResult.Channel.Agent)
			}
			if result.Posture == setupPostureManualShare {
				body.WriteString("note: writes stay local until you run `recoil channel publish` or `recoil channel outbox flush`.\n")
			}
			body.WriteString("next: recoil wake\n")
			return frontmatter(cmd.OutOrStdout(), []kv{
				{k: "tree", v: "project"},
				{k: "posture", v: result.Posture},
				{k: "sharing", v: setupSharingState(channelResult)},
				{k: "evidence", v: setupEvidenceState(result.Posture)},
				{k: "project_root", v: initResult.ProjectRoot},
				{k: "project_id", v: initResult.ProjectID},
				{k: "chunks", v: fmt.Sprintf("%d", mineResult.Chunks)},
				{k: "hooks", v: fmt.Sprintf("%d", len(hooks))},
				{k: "next_command", v: "recoil wake"},
			}, body.String())
		},
	}
	c.Flags().StringSliceVar(&setupOpts.agents, "agent", nil, "agent hook to install; repeat or comma-separate; defaults to auto-detect")
	c.Flags().BoolVar(&setupOpts.noHooks, "no-hooks", false, "skip agent hook installation")
	c.Flags().StringVar(&setupOpts.hookScope, "hook-scope", "project", "hook install scope: project or user")
	c.Flags().StringVar(&setupOpts.relay, "relay", "", "optional channel relay invite URL to join")
	c.Flags().StringVar(&setupOpts.relayAgent, "relay-agent", "", "agent name for optional relay join")
	c.Flags().BoolVar(&setupOpts.standalone, "standalone", false, "local memory tree only")
	c.Flags().BoolVar(&setupOpts.collaborative, "collaborative", false, "join sharing with automatic memory sharing")
	c.Flags().BoolVar(&setupOpts.manualShare, "manual-share", false, "join sharing but keep automatic memory sharing off")
	return c
}

func setupPosture(opts setupOptions) (string, error) {
	selected := 0
	for _, enabled := range []bool{opts.standalone, opts.collaborative, opts.manualShare} {
		if enabled {
			selected++
		}
	}
	if selected > 1 {
		return "", fmt.Errorf("choose only one posture: --standalone, --collaborative, or --manual-share")
	}
	hasRelay := strings.TrimSpace(opts.relay) != ""
	if opts.standalone && hasRelay {
		return "", fmt.Errorf("--standalone cannot be used with --relay")
	}
	if opts.collaborative && !hasRelay {
		return "", fmt.Errorf("--collaborative requires --relay")
	}
	if opts.manualShare && !hasRelay {
		return "", fmt.Errorf("--manual-share requires --relay")
	}
	switch {
	case opts.standalone:
		return setupPostureStandalone, nil
	case opts.manualShare:
		return setupPostureManualShare, nil
	case opts.collaborative || hasRelay:
		return setupPostureCollaborative, nil
	default:
		return "", nil
	}
}

func writeSetupPostureConfig(sc scope.Scope, posture string) error {
	path, err := config.ResolveSettingsPath(sc.Root)
	if err != nil {
		return err
	}
	settings, err := config.LoadSettings(path)
	if err != nil {
		return err
	}
	switch posture {
	case setupPostureStandalone:
		settings.Set("channel.auto_publish", channelAutoPublishOff)
		settings.Set("channel.jit_refresh", channelJITOff)
		settings.Set("session-evidence.enabled", "false")
	case setupPostureCollaborative:
		settings.Set("channel.auto_publish", channelAutoPublishGuidance)
		settings.Set("channel.jit_refresh", channelJITContext)
		settings.Set("session-evidence.enabled", "true")
	case setupPostureManualShare:
		settings.Set("channel.auto_publish", channelAutoPublishOff)
		settings.Set("channel.jit_refresh", channelJITContext)
		settings.Set("session-evidence.enabled", "false")
	default:
		return fmt.Errorf("unknown setup posture %q", posture)
	}
	return config.SaveSettings(path, settings)
}

func setupDisplayPosture(posture string, channelResult *channelJoinResult) string {
	if posture != "" {
		return posture
	}
	if channelResult == nil {
		return setupPostureStandalone
	}
	return "custom"
}

func setupSharingState(channelResult *channelJoinResult) string {
	if channelResult == nil {
		return "off"
	}
	return "connected"
}

func setupEvidenceState(posture string) string {
	if posture == setupPostureCollaborative {
		return "on"
	}
	return "off"
}

func runInitProject(ctx context.Context) (initResult, scope.Scope, error) {
	dbPath, err := config.ResolveDBPath(opts.dbPath)
	if err != nil {
		return initResult{}, scope.Scope{}, err
	}
	stateDir, err := config.ResolveStateDir()
	if err != nil {
		return initResult{}, scope.Scope{}, err
	}
	userIDPath, err := config.ResolveUserIDPath()
	if err != nil {
		return initResult{}, scope.Scope{}, err
	}
	userScope, err := scope.UserScope()
	if err != nil {
		return initResult{}, scope.Scope{}, err
	}
	projectScope, err := scope.InitProject(".")
	if err != nil {
		return initResult{}, scope.Scope{}, err
	}
	st, _, err := openStore()
	if err != nil {
		return initResult{}, scope.Scope{}, err
	}
	defer st.Close()
	if err := st.Repair(ctx); err != nil {
		return initResult{}, scope.Scope{}, err
	}
	return initResult{
		DBPath:        dbPath,
		StateDir:      stateDir,
		UserIDPath:    userIDPath,
		UserID:        userScope.ID,
		ProjectRoot:   projectScope.Root,
		ProjectID:     projectScope.ProjectID,
		ProjectMarker: projectScope.MarkerPath,
		FTS5:          true,
	}, projectScope, nil
}

func detectInstalledAgents() []string {
	candidates := []struct {
		name string
		bin  string
	}{
		{name: "codex", bin: "codex"},
		{name: "claude-code", bin: "claude"},
		{name: "opencode", bin: "opencode"},
	}
	var agents []string
	for _, candidate := range candidates {
		if _, err := exec.LookPath(candidate.bin); err == nil {
			agents = append(agents, candidate.name)
		}
	}
	return agents
}

func normalizedSetupAgents(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part == "" || seen[part] {
				continue
			}
			seen[part] = true
			out = append(out, part)
		}
	}
	return out
}

func firstSetupAgent(agents []string) string {
	if len(agents) == 0 {
		return ""
	}
	return agents[0]
}

func mineDefaultMaxFileBytes() int64 { return 256 * 1024 }

func mineDefaultMaxChunkChars() int { return 3000 }
