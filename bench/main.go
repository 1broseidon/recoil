// bench/main.go — External benchmark harness for recoil.
//
// Distinct from `recoil eval` (the in-CLI coherence gate). This harness
// measures recoil's retrieval against published memory benchmarks
// (LongMemEval, eventually LoCoMo / ConvoMem / MemBench).
//
// Usage:
//
//	CGO_CFLAGS="-DSQLITE_ENABLE_FTS5" go run ./bench longmemeval
//	go run ./bench longmemeval --data /path/to/longmemeval_s_cleaned.json
//	go run ./bench longmemeval --limit 25
//	go run ./bench longmemeval --top-k 10
package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd := os.Args[1]
	args := os.Args[2:]
	switch cmd {
	case "longmemeval":
		if err := runLongMemEval(args); err != nil {
			fmt.Fprintf(os.Stderr, "longmemeval: %v\n", err)
			os.Exit(1)
		}
	case "longmemeval-qa":
		if err := runLongMemEvalQA(args); err != nil {
			fmt.Fprintf(os.Stderr, "longmemeval-qa: %v\n", err)
			os.Exit(1)
		}
	case "longmemeval-grade":
		if err := runLongMemEvalGrade(args); err != nil {
			fmt.Fprintf(os.Stderr, "longmemeval-grade: %v\n", err)
			os.Exit(1)
		}
	case "locomo":
		if err := runLoCoMo(args); err != nil {
			fmt.Fprintf(os.Stderr, "locomo: %v\n", err)
			os.Exit(1)
		}
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n\n", cmd)
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "recoil bench harness")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Subcommands:")
	fmt.Fprintln(os.Stderr, "  longmemeval         LongMemEval retrieval (recall@5, recall@10, per-type)")
	fmt.Fprintln(os.Stderr, "  longmemeval-qa      LongMemEval QA: retrieval -> answerer LLM -> hypothesis JSONL")
	fmt.Fprintln(os.Stderr, "  longmemeval-grade   Grade hypothesis JSONL with LLM-as-judge (paper-exact prompts)")
	fmt.Fprintln(os.Stderr, "  locomo              LoCoMo retrieval (turn-grain + session-grain recall@5/10, per-category)")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Run with -h on any subcommand for its flags.")
}

// parseFlags is a small helper so each subcommand handler can declare its flags inline.
func parseFlags(name string, args []string, register func(*flag.FlagSet)) (*flag.FlagSet, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	register(fs)
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	return fs, nil
}
