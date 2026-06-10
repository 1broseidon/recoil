package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"
)

func TestHandleErrorWritesJSONEnvelopeAndExitCode(t *testing.T) {
	oldOpts := opts
	opts = globalOptions{json: true}
	defer func() { opts = oldOpts }()

	var out bytes.Buffer
	code := HandleError(&out, fmt.Errorf("query is required"))
	if code != exitValidation {
		t.Fatalf("expected validation exit %d, got %d", exitValidation, code)
	}
	var got errorEnvelope
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Kind != "error" || got.Error.Code != "VALIDATION" || got.Error.Message != "query is required" {
		t.Fatalf("unexpected error envelope: %+v", got)
	}
}
