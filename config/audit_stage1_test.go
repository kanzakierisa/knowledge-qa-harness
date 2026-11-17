package config

import (
	"strings"
	"testing"
)

func TestAuditStage1RequiredVariables(t *testing.T) {
	required := []string{"LLM_BASE_URL", "LLM_API_KEY", "LLM_CHAT_MODEL", "LLM_EMBED_MODEL", "MYSQL_DSN"}
	for _, omitted := range required {
		t.Run(omitted, func(t *testing.T) {
			t.Setenv("LLM_PROVIDER", "openai")
			t.Setenv("EMBED_BASE_URL", "")
			t.Setenv("EMBED_API_KEY", "")
			for _, key := range required {
				t.Setenv(key, "audit-local-placeholder")
			}
			t.Setenv(omitted, "")
			_, err := Load()
			if err == nil {
				t.Errorf("missing required variable %s silently accepted", omitted)
			} else if !strings.Contains(err.Error(), omitted) {
				t.Errorf("error does not identify missing %s: %s", omitted, err)
			}
		})
	}
}
