package cmd

import (
	"context"
	"strings"

	"github.com/1broseidon/recoil/internal/scope"
	"github.com/1broseidon/recoil/internal/store"
)

func autoSupersedeClaimFamily(ctx context.Context, st *store.Store, sc scope.Scope, claimKey, newMemID string) ([]string, error) {
	claimKey = strings.TrimSpace(claimKey)
	newMemID = strings.TrimSpace(newMemID)
	if claimKey == "" || newMemID == "" {
		return nil, nil
	}
	memories, err := st.List(ctx, store.ListParams{
		ClaimKey:  claimKey,
		ScopeKind: sc.Kind,
		ScopeID:   sc.ID,
		Limit:     20,
		Lifecycle: store.LifecycleCurrent,
	})
	if err != nil {
		return nil, err
	}
	var superseded []string
	for _, mem := range memories {
		if mem.ID == newMemID || isHistoricalMemory(mem) {
			continue
		}
		params := lifecycleParamsFromMemory(mem)
		params.Validity = "superseded"
		params.ClaimKey = mem.ClaimKey
		params.SupersededBy = newMemID
		updated, err := st.UpdateLifecycle(ctx, params)
		if err != nil {
			return superseded, err
		}
		superseded = append(superseded, updated.ID)
	}
	return superseded, nil
}
