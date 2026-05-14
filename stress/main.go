// stress is a non-benchmark stress-test harness for recoil. It exercises the
// CLI binary against real cloned repositories (path-pinned in manifest.json)
// across a query battery, multiple config variants, lifecycle scenarios,
// adversarial cases, and latency sweeps.
//
// Subcommands:
//
//	go run ./stress run-repo <key> [--config <name>] [--label <name>] [--mine-limit N] [--include-hidden]
//	go run ./stress aggregate [--label <name>] [--floor 0.99]
//	go run ./stress phase3
//	go run ./stress phase4
//	go run ./stress phase5
//	go run ./stress phase6
//	go run ./stress phase7
//	go run ./stress synth <repo-key> [--n 6] [--seed 42]
//	go run ./stress report
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "run-repo":
		cmdRunRepo(os.Args[2:])
	case "aggregate":
		cmdAggregate(os.Args[2:])
	case "audit":
		cmdAudit(os.Args[2:])
	case "apply-audit":
		cmdApplyAudit(os.Args[2:])
	case "phase2b":
		cmdPhase2b(os.Args[2:])
	case "phase3":
		cmdPhase3(os.Args[2:])
	case "phase4":
		cmdPhase4(os.Args[2:])
	case "phase5":
		cmdPhase5(os.Args[2:])
	case "phase6":
		cmdPhase6(os.Args[2:])
	case "phase7":
		cmdPhase7(os.Args[2:])
	case "synth":
		cmdSynth(os.Args[2:])
	case "report":
		cmdReport(os.Args[2:])
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `recoil stress harness

Subcommands:
  run-repo <key>      Run universal battery on one repo.
  aggregate           Build matrix from per-repo reports.
  phase2b             Phase 2b: rerun all 12 repos with synthesized transcripts.
  phase3              Config-surface sweep on 4 repos.
  phase4              Workflow/lifecycle stress on recoil clone.
  phase5              Adversarial cases on a seeded DB.
  phase6              Latency at 1k/10k/50k chunks.
  phase7              mempalace sanity check on Next.js clone.
  synth <key>         Synthesize transcripts for a repo's clone.
  report              Assemble REPORT.md from all phase outputs.

All paths and SHAs come from stress/manifest.json.
Clones live at $RECOIL_CLONES_ROOT (default ~/recoil-stress-clones).`)
}
