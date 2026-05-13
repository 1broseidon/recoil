package main

import (
	"fmt"
	"os"
	"path/filepath"
)

const defaultLongMemEvalFile = "longmemeval_s_cleaned.json"

func corpusRoot() (string, error) {
	exe, err := os.Executable()
	if err == nil {
		// When invoked via `go run`, exe lives in a temp dir; fall back to cwd.
		if _, err := os.Stat(filepath.Join(filepath.Dir(exe), ".corpus")); err == nil {
			return filepath.Join(filepath.Dir(exe), ".corpus"), nil
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for dir := cwd; dir != "/" && dir != ""; dir = filepath.Dir(dir) {
		candidate := filepath.Join(dir, "bench", ".corpus")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate, nil
		}
		// Or directly inside a bench/ workdir.
		if filepath.Base(dir) == "bench" {
			if info, err := os.Stat(filepath.Join(dir, ".corpus")); err == nil && info.IsDir() {
				return filepath.Join(dir, ".corpus"), nil
			}
		}
	}
	return "", fmt.Errorf("could not locate bench/.corpus; run from recoil repo or pass --data")
}

func resolveDataPath(override, defaultFile string) (string, error) {
	if override != "" {
		if _, err := os.Stat(override); err != nil {
			return "", fmt.Errorf("--data %q: %w", override, err)
		}
		return override, nil
	}
	root, err := corpusRoot()
	if err != nil {
		return "", err
	}
	path := filepath.Join(root, defaultFile)
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("dataset not found at %s; download with:\n  curl -fSL -o %s https://huggingface.co/datasets/xiaowu0162/longmemeval-cleaned/resolve/main/%s",
			path, path, defaultFile)
	}
	return path, nil
}

func resultsDir() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for dir := cwd; dir != "/" && dir != ""; dir = filepath.Dir(dir) {
		candidate := filepath.Join(dir, "bench")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			out := filepath.Join(candidate, "results")
			if err := os.MkdirAll(out, 0o755); err != nil {
				return "", err
			}
			return out, nil
		}
	}
	out := filepath.Join(cwd, "bench-results")
	if err := os.MkdirAll(out, 0o755); err != nil {
		return "", err
	}
	return out, nil
}
