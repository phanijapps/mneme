package domain

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestErrorCodeMappings(t *testing.T) {
	tests := []struct {
		code ErrorCode
		http int
	}{
		{CodeValidationErr, 400},
		{CodeSpaceAccessDenied, 403},
		{CodeMemoryNotFound, 404},
		{CodeVersionConflict, 409},
		{CodeSlotBudgetExceeded, 409},
		{CodeTokenBudgetExceeded, 409},
		{CodeMemorySuperseded, 410},
		{CodeLinkIntegrity, 422},
		{CodeRecallBudgetInvalid, 422},
		{CodeRateLimited, 429},
		{CodeStoreUnavailable, 503},
	}
	for _, tt := range tests {
		t.Run(string(tt.code), func(t *testing.T) {
			assert.Equal(t, tt.http, tt.code.HTTPStatus())
		})
	}
}

func TestSentinelErrors(t *testing.T) {
	sentinels := []error{
		ErrNotFound, ErrValidation, ErrVersionConflict, ErrSlotBudgetExceeded,
		ErrTokenBudgetExceeded, ErrSpaceAccessDenied, ErrDuplicateProposal,
		ErrMemoryDeleted, ErrInvalidState,
	}
	for _, s := range sentinels {
		require.Error(t, s)
		de, ok := s.(*Error)
		require.True(t, ok, "%v should be *domain.Error", s)
		assert.NotEmpty(t, de.GetCode())
		assert.NotEmpty(t, de.Message)
	}
}

func TestErrorWrapAndFromError(t *testing.T) {
	base := &Error{Code: CodeMemoryNotFound, Message: "memory 42 does not exist",
		Details: map[string]any{"id": "42"}}
	wrapped := fmt.Errorf("GetByID: %w", base)
	got := FromError(wrapped)
	require.Equal(t, CodeMemoryNotFound, got.Code)
	assert.Equal(t, "42", got.Details["id"])

	unknown := FromError(errors.New("disk on fire"))
	require.Equal(t, CodeInternal, unknown.Code)

	assert.Nil(t, FromError(nil))
}

func TestWithDetailDoesNotMutateOriginal(t *testing.T) {
	base := &Error{Code: CodeVersionConflict, Message: "conflict",
		Details: map[string]any{"expected_version": 3}}
	derived := base.WithDetail("current_version", 4)
	assert.Equal(t, 4, derived.Details["current_version"])
	assert.NotContains(t, base.Details, "current_version")
	require.Equal(t, 1, len(base.Details))
}

func TestErrorResponseEnvelope(t *testing.T) {
	body := NewErrorResponse(
		&Error{Code: CodeMemoryNotFound, Message: "Memory x does not exist",
			Details: map[string]any{"id": "x"}},
		"req_01J8ZQ")
	assert.Equal(t, CodeMemoryNotFound, body.Error.Code)
	assert.Equal(t, "req_01J8ZQ", body.RequestID)
	assert.Contains(t, body.DocURL, string(CodeMemoryNotFound))
	assert.Equal(t, "x", body.Error.Details["id"])
}

func TestNewValidationError(t *testing.T) {
	err := NewValidationError("Memory.Type", "bogus", "episodic|semantic|procedural")
	require.Equal(t, CodeValidationErr, err.Code)
	assert.Equal(t, "Memory.Type", err.Details["field"])
	assert.Contains(t, err.Error(), "VALIDATION_ERROR")
}
