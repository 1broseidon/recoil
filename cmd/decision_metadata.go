package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/1broseidon/recoil/internal/store"
)

const (
	decisionStanceKey  = "stance"
	decisionSubjectKey = "subject"
)

type decisionMetadata struct {
	Stance  string `json:"stance,omitempty"`
	Subject string `json:"subject,omitempty"`
}

func normalizeDecisionStance(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "", nil
	}
	switch value {
	case "prefers", "rejects", "requires", "forbids":
		return value, nil
	default:
		return "", fmt.Errorf("invalid stance %q (want: prefers, rejects, requires, forbids)", value)
	}
}

func mergeDecisionMetadata(raw, stance, subject string) (string, error) {
	metadata := map[string]any{}
	if strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &metadata); err != nil {
			return "", fmt.Errorf("--metadata must be a JSON object when decision metadata flags are used: %w", err)
		}
	}
	if strings.TrimSpace(stance) != "" {
		normalized, err := normalizeDecisionStance(stance)
		if err != nil {
			return "", err
		}
		metadata[decisionStanceKey] = normalized
	}
	if strings.TrimSpace(subject) != "" {
		metadata[decisionSubjectKey] = strings.TrimSpace(subject)
	}
	data, err := json.Marshal(metadata)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func decisionMetadataFromMemory(mem store.Memory) decisionMetadata {
	if strings.TrimSpace(mem.MetadataJSON) == "" {
		return decisionMetadata{}
	}
	var metadata map[string]any
	if err := json.Unmarshal([]byte(mem.MetadataJSON), &metadata); err != nil {
		return decisionMetadata{}
	}
	return decisionMetadata{
		Stance:  metadataStringValue(metadata, decisionStanceKey),
		Subject: metadataStringValue(metadata, decisionSubjectKey),
	}
}

func metadataStringValue(metadata map[string]any, key string) string {
	value, ok := metadata[key]
	if !ok {
		return ""
	}
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	default:
		return ""
	}
}
