package app

import "testing"

func TestNormalizeOnboardingTokensUsesTokenPrefix(t *testing.T) {
	mode, bindToken, agentToken := NormalizeOnboardingTokens("", "b_bind", "")
	if mode != OnboardingModeNew || bindToken != "b_bind" || agentToken != "" {
		t.Fatalf("bind token mode mismatch: mode=%q bind=%q agent=%q", mode, bindToken, agentToken)
	}

	mode, bindToken, agentToken = NormalizeOnboardingTokens(OnboardingModeNew, "t_agent", "")
	if mode != OnboardingModeExisting || bindToken != "" || agentToken != "t_agent" {
		t.Fatalf("agent token mode mismatch: mode=%q bind=%q agent=%q", mode, bindToken, agentToken)
	}
}
