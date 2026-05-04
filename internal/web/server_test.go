package web

import (
	"strings"
	"testing"
)

func TestOnboardingFormUsesTokenPrefixMode(t *testing.T) {
	t.Parallel()

	data, err := assets.ReadFile("templates/index.html")
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	body := string(data)

	for _, want := range []string{
		`id="onboarding-mode-field" type="hidden" name="agent_mode" value="existing"`,
		`name="hub_region"`,
		`id="onboarding-token-input" type="password"`,
		`const tokenMode = (value) => String(value || "").trim().toLowerCase().startsWith("b_") ? "new" : "existing";`,
		`handleField.hidden = bound || !isNew;`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected template to contain %q", want)
		}
	}
	if strings.Contains(body, `onboarding-existing-agent-toggle`) || strings.Contains(body, `onboarding-new-agent-toggle`) {
		t.Fatal("did not expect explicit onboarding mode toggles")
	}
}
