package cmd

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1broseidon/recoil/internal/scope"
	"github.com/1broseidon/recoil/internal/store"
)

func TestProfileCommandBuildsEntityProfileAndSearchUsesIt(t *testing.T) {
	oldOpts := opts
	defer func() { opts = oldOpts }()

	root := t.TempDir()
	sc, err := scope.InitProject(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	dbPath := filepath.Join(t.TempDir(), "recoil.db")
	opts = globalOptions{dbPath: dbPath}

	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = st.AddMemory(context.Background(), store.AddMemoryParams{
		Role:       "source",
		Content:    "Caroline researches adoption agencies and is preparing a counseling plan.",
		SourceKind: "session_evidence",
		SourcePath: "session-evidence/test.jsonl",
		SourceRef:  "turn 1-2",
		ScopeKind:  sc.Kind,
		ScopeID:    sc.ID,
		ProjectID:  sc.ProjectID,
		Validity:   "active",
	})
	if closeErr := st.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if err != nil {
		t.Fatal(err)
	}

	profileCmd := newProfileCommand()
	var profileOut bytes.Buffer
	profileCmd.SetOut(&profileOut)
	profileCmd.SetArgs([]string{"--entity", "Caroline"})
	if err := profileCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(profileOut.String(), "Profile - Caroline") {
		t.Fatalf("expected profile output, got:\n%s", profileOut.String())
	}

	searchCmd := newSearchCommand()
	var searchOut bytes.Buffer
	searchCmd.SetOut(&searchOut)
	searchCmd.SetArgs([]string{"--limit", "3", "what is Caroline researching?"})
	if err := searchCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(searchOut.String(), "source_kind: entity_profile") ||
		!strings.Contains(searchOut.String(), "Profile - Caroline") {
		t.Fatalf("expected profile search result, got:\n%s", searchOut.String())
	}
}
