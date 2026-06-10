package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	exitGeneric      = 1
	exitValidation   = 2
	exitNotFound     = 3
	exitUpstream     = 4
	exitPrecondition = 5
	exitCancelled    = 6
)

type errorEnvelope struct {
	Version string       `json:"version"`
	Kind    string       `json:"kind"`
	Error   errorPayload `json:"error"`
}

type errorPayload struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

func HandleError(w io.Writer, err error) int {
	code, exitCode := classifyError(err)
	if opts.json {
		_ = writeErrorJSON(w, code, err)
		return exitCode
	}
	_, _ = fmt.Fprintln(w, err)
	return exitCode
}

func writeErrorJSON(w io.Writer, code string, err error) error {
	enc := json.NewEncoder(w)
	return enc.Encode(errorEnvelope{
		Version: "0.1",
		Kind:    "error",
		Error: errorPayload{
			Code:    code,
			Message: err.Error(),
		},
	})
}

func classifyError(err error) (string, int) {
	if err == nil {
		return "OK", 0
	}
	if errors.Is(err, context.Canceled) {
		return "CANCELLED", exitCancelled
	}
	msg := strings.ToLower(err.Error())
	switch {
	case containsAny(msg, "unknown flag", "required", " is empty", "cannot both", "choose ", "invalid ", "accepts ", "requires ", "must be ", "provide "):
		return "VALIDATION", exitValidation
	case containsAny(msg, "not found", "no such memory", "no memory", "not set", "unknown config key"):
		return "NOT_FOUND", exitNotFound
	case containsAny(msg, "session evidence is disabled", "not initialized", "precondition"):
		return "PRECONDITION", exitPrecondition
	case containsAny(msg, "database", "sqlite", "fts5", "unable to open", "embedding provider", "network", "http"):
		return "UPSTREAM", exitUpstream
	default:
		return "ERROR", exitGeneric
	}
}
