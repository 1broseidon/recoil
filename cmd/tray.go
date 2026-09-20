package cmd

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"fyne.io/systray"
	"github.com/1broseidon/recoil/internal/trayautostart"
	"github.com/1broseidon/recoil/internal/traystats"
	"github.com/spf13/cobra"
)

type trayOptions struct {
	project      string
	allScopes    bool
	interval     time.Duration
	wakeLimit    int
	wakeMaxChars int
}

type trayAutostartOptions struct {
	project      string
	interval     time.Duration
	wakeLimit    int
	wakeMaxChars int
}

func newTrayCommand() *cobra.Command {
	var trayOpts trayOptions
	c := &cobra.Command{
		Use:   "tray",
		Short: "Run the Recoil system tray companion",
		Long: `Run a small background system tray companion.

The tray process is the long-running component. Recoil's normal CLI commands
remain one-shot: the tray reads the database periodically and only invokes the
CLI for explicit actions such as copying wake context.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if trayOpts.interval <= 0 {
				return fmt.Errorf("--interval must be positive")
			}
			if trayOpts.wakeLimit <= 0 {
				return fmt.Errorf("--wake-limit must be positive")
			}
			target, err := traystats.ResolveTarget(traystats.ResolveOptions{
				DBPath:    opts.dbPath,
				Project:   trayOpts.project,
				AllScopes: trayOpts.allScopes,
			})
			if err != nil {
				return err
			}
			return runTray(cmd.Context(), target, trayOpts)
		},
	}
	c.Flags().StringVar(&trayOpts.project, "project", "", "project path to pin in the tray (default: current directory)")
	c.Flags().BoolVar(&trayOpts.allScopes, "all-scopes", false, "show aggregate stats across all scopes")
	c.Flags().DurationVar(&trayOpts.interval, "interval", 15*time.Second, "tray refresh interval")
	c.Flags().IntVar(&trayOpts.wakeLimit, "wake-limit", 8, "maximum memories to include when copying wake context")
	c.Flags().IntVar(&trayOpts.wakeMaxChars, "wake-max-chars", 2400, "maximum wake context characters to copy")
	c.AddCommand(newTrayAutostartCommand())
	return c
}

func newTrayAutostartCommand() *cobra.Command {
	c := &cobra.Command{
		Use:   "autostart",
		Short: "Manage OS login startup for the tray companion",
		Long: `Manage login startup for the tray companion.

Install writes the OS-native per-user startup entry:
  Linux   XDG autostart .desktop file
  macOS   LaunchAgent plist
  Windows Startup-folder launcher`,
	}
	c.AddCommand(newTrayAutostartInstallCommand())
	c.AddCommand(newTrayAutostartStatusCommand())
	c.AddCommand(newTrayAutostartUninstallCommand())
	return c
}

func newTrayAutostartInstallCommand() *cobra.Command {
	autoOpts := defaultTrayAutostartOptions()
	c := &cobra.Command{
		Use:   "install",
		Short: "Start Recoil tray automatically at login",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			config, err := trayAutostartConfig(autoOpts)
			if err != nil {
				return err
			}
			status, err := trayautostart.Install(config)
			if err != nil {
				return err
			}
			return writeTrayAutostartResult(cmd, "tray_autostart_install_result", status)
		},
	}
	addTrayAutostartInstallFlags(c, &autoOpts)
	return c
}

func newTrayAutostartStatusCommand() *cobra.Command {
	autoOpts := defaultTrayAutostartOptions()
	c := &cobra.Command{
		Use:   "status",
		Short: "Show tray autostart status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			config, err := trayAutostartConfig(autoOpts)
			if err != nil {
				return err
			}
			status, err := trayautostart.Check(config)
			if err != nil {
				return err
			}
			return writeTrayAutostartResult(cmd, "tray_autostart_status_result", status)
		},
	}
	return c
}

func newTrayAutostartUninstallCommand() *cobra.Command {
	autoOpts := defaultTrayAutostartOptions()
	c := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove Recoil tray login startup",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			config, err := trayAutostartConfig(autoOpts)
			if err != nil {
				return err
			}
			status, err := trayautostart.Uninstall(config)
			if err != nil {
				return err
			}
			return writeTrayAutostartResult(cmd, "tray_autostart_uninstall_result", status)
		},
	}
	return c
}

func defaultTrayAutostartOptions() trayAutostartOptions {
	return trayAutostartOptions{
		interval:     15 * time.Second,
		wakeLimit:    8,
		wakeMaxChars: 2400,
	}
}

func addTrayAutostartInstallFlags(c *cobra.Command, opts *trayAutostartOptions) {
	c.Flags().StringVar(&opts.project, "project", "", "project path to pin at login (default: all scopes)")
	c.Flags().DurationVar(&opts.interval, "interval", opts.interval, "tray refresh interval")
	c.Flags().IntVar(&opts.wakeLimit, "wake-limit", opts.wakeLimit, "maximum memories to include when copying wake context")
	c.Flags().IntVar(&opts.wakeMaxChars, "wake-max-chars", opts.wakeMaxChars, "maximum wake context characters to copy")
}

func runTray(parent context.Context, target traystats.Target, opts trayOptions) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	signals := make(chan os.Signal, 2)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	go func() {
		select {
		case <-signals:
			systray.Quit()
		case <-ctx.Done():
		}
	}()

	icons := recoilTrayIcons()
	onReady := func() {
		setTrayIcon(icons.idle)
		systray.SetTitle("")
		systray.SetTooltip("Recoil starting")

		ui := newTrayUI(target, opts, icons)
		ui.refresh(ctx)
		go ui.run(ctx)
		go func() {
			ticker := time.NewTicker(opts.interval)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					ui.refresh(ctx)
				case <-ctx.Done():
					return
				}
			}
		}()
	}
	onExit := func() {
		cancel()
	}
	systray.Run(onReady, onExit)
	return nil
}

func trayAutostartConfig(opts trayAutostartOptions) (trayautostart.Config, error) {
	exe, err := os.Executable()
	if err != nil {
		return trayautostart.Config{}, err
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return trayautostart.Config{}, err
	}
	args := []string{"tray"}
	project := strings.TrimSpace(opts.project)
	if project == "" {
		args = append(args, "--all-scopes")
	} else {
		project, err = filepath.Abs(project)
		if err != nil {
			return trayautostart.Config{}, err
		}
		args = append(args, "--project", project)
	}
	if opts.interval <= 0 {
		return trayautostart.Config{}, fmt.Errorf("--interval must be positive")
	}
	if opts.wakeLimit <= 0 {
		return trayautostart.Config{}, fmt.Errorf("--wake-limit must be positive")
	}
	if opts.wakeMaxChars <= 0 {
		return trayautostart.Config{}, fmt.Errorf("--wake-max-chars must be positive")
	}
	args = append(args,
		"--interval", opts.interval.String(),
		"--wake-limit", strconv.Itoa(opts.wakeLimit),
		"--wake-max-chars", strconv.Itoa(opts.wakeMaxChars),
	)
	return trayautostart.Config{
		AppID:      "com.1broseidon.recoil.tray",
		Name:       "Recoil Tray",
		Executable: exe,
		Arguments:  args,
	}, nil
}

func writeTrayAutostartResult(cmd *cobra.Command, kind string, status trayautostart.Status) error {
	if opts.json {
		return writeJSON(cmd.OutOrStdout(), kind, status)
	}
	return frontmatter(cmd.OutOrStdout(), []kv{
		{k: "platform", v: status.Platform},
		{k: "installed", v: fmt.Sprintf("%t", status.Installed)},
		{k: "path", v: status.Path},
		{k: "command", v: strings.Join(status.Command, " ")},
	}, status.Message+"\n")
}

type trayUI struct {
	target traystats.Target
	opts   trayOptions
	icons  trayIcons

	mu       sync.Mutex
	status   *systray.MenuItem
	scope    *systray.MenuItem
	counts   *systray.MenuItem
	activity *systray.MenuItem
	sources  *systray.MenuItem
	agents   *systray.MenuItem
	copyWake *systray.MenuItem
	refreshM *systray.MenuItem
	quit     *systray.MenuItem
}

type trayIcons struct {
	idle      []byte
	attention []byte
}

func newTrayUI(target traystats.Target, opts trayOptions, icons trayIcons) *trayUI {
	ui := &trayUI{
		target: target,
		opts:   opts,
		icons:  icons,
	}
	ui.status = systray.AddMenuItem("Recoil: starting", "Current Recoil status")
	ui.status.Disable()
	ui.scope = systray.AddMenuItem("Scope: resolving", "Scope shown in the tray")
	ui.scope.Disable()
	ui.counts = systray.AddMenuItem("Memories: resolving", "Current, stale, and total memory counts")
	ui.counts.Disable()
	ui.activity = systray.AddMenuItem("Activity: resolving", "Recent memory activity")
	ui.activity.Disable()
	ui.sources = systray.AddMenuItem("Sources: resolving", "Tracked source count")
	ui.sources.Disable()
	ui.agents = systray.AddMenuItem("Active agents: resolving", "Recently seen Recoil agent sessions")
	ui.agents.Disable()
	systray.AddSeparator()
	ui.copyWake = systray.AddMenuItem("Copy wake context", "Copy bounded Recoil wake context to the clipboard")
	if target.AllScopes {
		ui.copyWake.Disable()
	}
	ui.refreshM = systray.AddMenuItem("Refresh now", "Refresh tray stats now")
	systray.AddSeparator()
	ui.quit = systray.AddMenuItem("Quit", "Quit Recoil tray")
	return ui
}

func (ui *trayUI) run(ctx context.Context) {
	for {
		select {
		case <-ui.refreshM.ClickedCh:
			go ui.refresh(ctx)
		case <-ui.copyWake.ClickedCh:
			go ui.copyWakeContext(ctx)
		case <-ui.quit.ClickedCh:
			systray.Quit()
			return
		case <-ctx.Done():
			return
		}
	}
}

func (ui *trayUI) refresh(ctx context.Context) {
	snapshot, err := traystats.Collect(ctx, ui.target, traystats.CollectOptions{
		Now:             time.Now(),
		AgentSessionTTL: 5 * time.Minute,
	})
	if err != nil {
		snapshot = traystats.Snapshot{
			DBPath:      ui.target.DBPath,
			Ready:       false,
			Message:     err.Error(),
			AllScopes:   ui.target.AllScopes,
			ProjectName: ui.target.ProjectName,
			ProjectRoot: ui.target.ProjectRoot,
		}
	}
	ui.setSnapshot(snapshot)
}

func (ui *trayUI) setSnapshot(snapshot traystats.Snapshot) {
	ui.mu.Lock()
	defer ui.mu.Unlock()

	if !snapshot.Ready {
		setTrayIcon(ui.icons.attention)
		ui.status.SetTitle(shortMenuText("Recoil: "+firstNonEmpty(snapshot.Message, "not ready"), 80))
		ui.scope.SetTitle(scopeTitle(snapshot))
		ui.counts.SetTitle("Memories: unavailable")
		ui.activity.SetTitle("Activity: unavailable")
		ui.sources.SetTitle("Sources: unavailable")
		ui.agents.SetTitle("Active agents: unavailable")
		ui.copyWake.Disable()
		systray.SetTooltip(shortMenuText("Recoil not ready: "+firstNonEmpty(snapshot.Message, "unknown error"), 240))
		return
	}

	if snapshot.StaleCount > 0 {
		setTrayIcon(ui.icons.attention)
	} else {
		setTrayIcon(ui.icons.idle)
	}
	ui.status.SetTitle("Recoil: ready")
	ui.scope.SetTitle(scopeTitle(snapshot))
	ui.counts.SetTitle(fmt.Sprintf("%d current · %d stale · %d total", snapshot.CurrentCount, snapshot.StaleCount, snapshot.TotalCount))
	ui.activity.SetTitle(fmt.Sprintf("+%d today · last %s", snapshot.Added24h, humanAge(snapshot.LastActivityAt, time.Now())))
	ui.sources.SetTitle(fmt.Sprintf("Sources: %d", snapshot.SourceCount))
	ui.agents.SetTitle(fmt.Sprintf("Active agents: %d", snapshot.ActiveSessions))
	if snapshot.AllScopes {
		ui.copyWake.Disable()
	} else {
		ui.copyWake.Enable()
	}
	systray.SetTooltip(trayTooltip(snapshot))
}

func (ui *trayUI) copyWakeContext(parent context.Context) {
	ui.mu.Lock()
	ui.copyWake.SetTitle("Copying wake context...")
	ui.copyWake.Disable()
	ui.mu.Unlock()

	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	defer cancel()
	text, err := runWakeContext(ctx, ui.target, ui.opts)
	if err == nil {
		err = copyToClipboard(ctx, text)
	}

	ui.mu.Lock()
	defer ui.mu.Unlock()
	ui.copyWake.SetTitle("Copy wake context")
	if ui.target.AllScopes {
		ui.copyWake.Disable()
	} else {
		ui.copyWake.Enable()
	}
	if err != nil {
		ui.status.SetTitle(shortMenuText("Recoil: copy failed", 80))
		systray.SetTooltip(shortMenuText("Copy wake context failed: "+err.Error(), 240))
		return
	}
	ui.status.SetTitle("Recoil: wake copied")
	systray.SetTooltip("Recoil wake context copied to clipboard")
}

func runWakeContext(ctx context.Context, target traystats.Target, opts trayOptions) (string, error) {
	if target.AllScopes {
		return "", fmt.Errorf("wake context requires a project scope")
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	args := []string{"--db", target.DBPath, "wake", "--limit", strconv.Itoa(opts.wakeLimit), "--max-chars", strconv.Itoa(opts.wakeMaxChars)}
	if target.ProjectRoot != "" {
		args = append(args, "--project", target.ProjectRoot)
	}
	cmd := exec.CommandContext(ctx, exe, args...)
	if target.ProjectRoot != "" {
		cmd.Dir = target.ProjectRoot
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, shortMenuText(strings.TrimSpace(string(out)), 180))
	}
	return string(out), nil
}

func copyToClipboard(ctx context.Context, text string) error {
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("wake context was empty")
	}
	switch runtime.GOOS {
	case "darwin":
		return commandWithInput(ctx, text, "pbcopy")
	case "windows":
		return commandWithInput(ctx, text, "clip")
	default:
		candidates := [][]string{
			{"wl-copy"},
			{"xclip", "-selection", "clipboard"},
			{"xsel", "--clipboard", "--input"},
		}
		for _, candidate := range candidates {
			if _, err := exec.LookPath(candidate[0]); err != nil {
				continue
			}
			return commandWithInput(ctx, text, candidate[0], candidate[1:]...)
		}
		return fmt.Errorf("no clipboard command found; install wl-copy, xclip, or xsel")
	}
}

func commandWithInput(ctx context.Context, text, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	if _, err := io.WriteString(stdin, text); err != nil {
		stdin.Close()
		return err
	}
	if err := stdin.Close(); err != nil {
		return err
	}
	if err := cmd.Wait(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("%w: %s", err, shortMenuText(msg, 120))
		}
		return err
	}
	return nil
}

func scopeTitle(snapshot traystats.Snapshot) string {
	if snapshot.AllScopes {
		return "Scope: all scopes"
	}
	name := firstNonEmpty(snapshot.ProjectName, snapshot.ScopeID, "project")
	if snapshot.ProjectInitialized {
		return shortMenuText("Project: "+name, 80)
	}
	return shortMenuText("Project: "+name+" (uninitialized)", 80)
}

func trayTooltip(snapshot traystats.Snapshot) string {
	lines := []string{
		"Recoil ready",
		scopeTitle(snapshot),
		fmt.Sprintf("%d current, %d stale, +%d today", snapshot.CurrentCount, snapshot.StaleCount, snapshot.Added24h),
		"Last activity: " + humanAge(snapshot.LastActivityAt, time.Now()),
	}
	if snapshot.ActiveSessions > 0 {
		lines = append(lines, fmt.Sprintf("Active agents: %d", snapshot.ActiveSessions))
	}
	return strings.Join(lines, "\n")
}

func humanAge(value string, now time.Time) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "never"
	}
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		if parsed, parseErr := time.Parse(time.RFC3339Nano, value); parseErr == nil {
			t = parsed
		} else {
			return "unknown"
		}
	}
	if now.IsZero() {
		now = time.Now()
	}
	d := now.Sub(t)
	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	if d < 48*time.Hour {
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}

func setTrayIcon(icon []byte) {
	if runtime.GOOS == "darwin" {
		systray.SetTemplateIcon(icon, icon)
		return
	}
	systray.SetIcon(icon)
}

func recoilTrayIcons() trayIcons {
	return trayIcons{
		idle:      trayIconPNG(drawPinnedProjectGlyph),
		attention: trayIconPNG(drawRecallSparkGlyph),
	}
}

func trayIconPNG(draw func(*image.NRGBA, color.NRGBA)) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, 22, 22))
	draw(img, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil
	}
	return buf.Bytes()
}

func drawPinnedProjectGlyph(img *image.NRGBA, ink color.NRGBA) {
	fillDisk(img, 11, 8, 5, ink)
	fillTriangle(img, image.Point{X: 6, Y: 10}, image.Point{X: 16, Y: 10}, image.Point{X: 11, Y: 18}, ink)
	clear := color.NRGBA{}
	fillDisk(img, 11, 8, 2, clear)
}

func drawRecallSparkGlyph(img *image.NRGBA, ink color.NRGBA) {
	fillTriangle(img, image.Point{X: 11, Y: 2}, image.Point{X: 14, Y: 9}, image.Point{X: 8, Y: 9}, ink)
	fillTriangle(img, image.Point{X: 11, Y: 20}, image.Point{X: 8, Y: 13}, image.Point{X: 14, Y: 13}, ink)
	fillTriangle(img, image.Point{X: 2, Y: 11}, image.Point{X: 9, Y: 8}, image.Point{X: 9, Y: 14}, ink)
	fillTriangle(img, image.Point{X: 20, Y: 11}, image.Point{X: 13, Y: 14}, image.Point{X: 13, Y: 8}, ink)
	fillDisk(img, 11, 11, 3, ink)
}

func fillDisk(img *image.NRGBA, cx, cy, r int, c color.NRGBA) {
	rr := r * r
	for y := cy - r; y <= cy+r; y++ {
		for x := cx - r; x <= cx+r; x++ {
			dx := x - cx
			dy := y - cy
			if dx*dx+dy*dy <= rr {
				setPixel(img, x, y, c)
			}
		}
	}
}

func fillTriangle(img *image.NRGBA, a, b, c image.Point, ink color.NRGBA) {
	minX := glyphMinInt(a.X, glyphMinInt(b.X, c.X))
	maxX := glyphMaxInt(a.X, glyphMaxInt(b.X, c.X))
	minY := glyphMinInt(a.Y, glyphMinInt(b.Y, c.Y))
	maxY := glyphMaxInt(a.Y, glyphMaxInt(b.Y, c.Y))
	area := edge(a, b, c)
	if area == 0 {
		return
	}
	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			p := image.Point{X: x, Y: y}
			w0 := edge(b, c, p)
			w1 := edge(c, a, p)
			w2 := edge(a, b, p)
			if (w0 >= 0 && w1 >= 0 && w2 >= 0) || (w0 <= 0 && w1 <= 0 && w2 <= 0) {
				setPixel(img, x, y, ink)
			}
		}
	}
}

func edge(a, b, c image.Point) int {
	return (c.X-a.X)*(b.Y-a.Y) - (c.Y-a.Y)*(b.X-a.X)
}

func setPixel(img *image.NRGBA, x, y int, c color.NRGBA) {
	if x < img.Bounds().Min.X || x >= img.Bounds().Max.X || y < img.Bounds().Min.Y || y >= img.Bounds().Max.Y {
		return
	}
	img.SetNRGBA(x, y, c)
}

func glyphMinInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func glyphMaxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func shortMenuText(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if max <= 0 || len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return strings.TrimSpace(s[:max-3]) + "..."
}
