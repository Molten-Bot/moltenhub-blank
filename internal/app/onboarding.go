package app

import (
	"errors"
	"strings"
)

const (
	OnboardingStepBind         = "bind"
	OnboardingStepWorkBind     = "work_bind"
	OnboardingStepProfileSet   = "profile_set"
	OnboardingStepWorkActivate = "work_activate"
)

type OnboardingStep struct {
	ID     string
	Label  string
	Status string
	Detail string
}

type OnboardingError struct {
	Stage string
	Err   error
}

func (e *OnboardingError) Error() string {
	if e == nil || e.Err == nil {
		return "onboarding failed"
	}
	return e.Err.Error()
}

func (e *OnboardingError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func WrapOnboardingError(stage string, err error) error {
	if err == nil {
		return nil
	}
	if stage == "" {
		stage = OnboardingStepBind
	}
	return &OnboardingError{Stage: stage, Err: err}
}

func OnboardingStageFromError(err error) string {
	var onboardingErr *OnboardingError
	if errors.As(err, &onboardingErr) && strings.TrimSpace(onboardingErr.Stage) != "" {
		return strings.TrimSpace(onboardingErr.Stage)
	}
	return OnboardingStepBind
}

func NormalizeOnboardingTokens(mode, bindToken, agentToken string) (string, string, string) {
	bindToken = strings.TrimSpace(bindToken)
	agentToken = strings.TrimSpace(agentToken)
	submitted := bindToken
	if submitted == "" {
		submitted = agentToken
	}
	if strings.HasPrefix(strings.ToLower(submitted), "b_") {
		return OnboardingModeNew, submitted, ""
	}
	if submitted != "" {
		return OnboardingModeExisting, "", submitted
	}
	if strings.EqualFold(strings.TrimSpace(mode), OnboardingModeNew) {
		return OnboardingModeNew, bindToken, ""
	}
	return OnboardingModeExisting, "", agentToken
}

func DefaultOnboardingStepsForMode(mode string) []OnboardingStep {
	steps := []OnboardingStep{
		{ID: OnboardingStepBind, Label: "Redeem Token", Status: "pending", Detail: "Create this blank app credential."},
		{ID: OnboardingStepWorkBind, Label: "Verify", Status: "pending", Detail: "Check hub credential over HTTP."},
		{ID: OnboardingStepProfileSet, Label: "Register", Status: "pending", Detail: "Register blank app metadata."},
		{ID: OnboardingStepWorkActivate, Label: "Activate", Status: "pending", Detail: "Stabilize HTTP/WebSocket connection."},
	}
	if strings.EqualFold(strings.TrimSpace(mode), OnboardingModeExisting) {
		steps[0].Label = "Agent Token"
		steps[0].Detail = "Check existing agent token."
	}
	return steps
}
