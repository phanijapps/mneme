package domain

import (
	"errors"
	"fmt"
)

// ErrorCode is the stable machine code of a domain error; values mirror the
// REST/MCP error taxonomy of api-contracts §1.3 so both protocols share one
// taxonomy.
type ErrorCode string

const (
	CodeValidationErr            ErrorCode = "VALIDATION_ERROR"
	CodeUnauthenticated          ErrorCode = "UNAUTHENTICATED"
	CodeSpaceAccessDenied        ErrorCode = "SPACE_ACCESS_DENIED"
	CodeSpaceWriteDenied         ErrorCode = "SPACE_WRITE_DENIED"
	CodeSpacePromoteDenied       ErrorCode = "SPACE_PROMOTE_DENIED"
	CodeSpaceAdminRequired       ErrorCode = "SPACE_ADMIN_REQUIRED"
	CodePurgeForbidden           ErrorCode = "PURGE_FORBIDDEN"
	CodeLifecycleScopeForbidden  ErrorCode = "LIFECYCLE_SCOPE_FORBIDDEN"
	CodeMemoryNotFound           ErrorCode = "MEMORY_NOT_FOUND"
	CodeSessionNotFound          ErrorCode = "SESSION_NOT_FOUND"
	CodeSpaceNotFound            ErrorCode = "SPACE_NOT_FOUND"
	CodeRecallNotFound           ErrorCode = "RECALL_REQUEST_NOT_FOUND"
	CodeProposalNotFound         ErrorCode = "PROPOSAL_NOT_FOUND"
	CodeVersionConflict          ErrorCode = "VERSION_CONFLICT"
	CodeSessionAlreadyEnded      ErrorCode = "SESSION_ALREADY_ENDED"
	CodeProposalAlreadyResolved  ErrorCode = "PROPOSAL_ALREADY_RESOLVED"
	CodeSpaceSyncInProgress      ErrorCode = "SPACE_SYNC_IN_PROGRESS"
	CodeSpaceDiverged            ErrorCode = "SPACE_DIVERGED"
	CodeSlotBudgetExceeded       ErrorCode = "SLOT_BUDGET_EXCEEDED"
	CodeTokenBudgetExceeded      ErrorCode = "TOKEN_BUDGET_EXCEEDED"
	CodeMemoryExpired            ErrorCode = "MEMORY_EXPIRED"
	CodeMemorySuperseded         ErrorCode = "MEMORY_SUPERSEDED"
	CodeUnsupportedMediaType     ErrorCode = "UNSUPPORTED_MEDIA_TYPE"
	CodeLinkIntegrity            ErrorCode = "LINK_INTEGRITY"
	CodeSpacePolicyInvalid       ErrorCode = "SPACE_POLICY_INVALID"
	CodePromotionNotConsolidated ErrorCode = "PROMOTION_NOT_CONSOLIDATED"
	CodeRecallBudgetInvalid      ErrorCode = "RECALL_BUDGET_INVALID"
	CodeRateLimited              ErrorCode = "RATE_LIMITED"
	CodeInternal                 ErrorCode = "INTERNAL"
	CodeStoreUnavailable         ErrorCode = "STORE_UNAVAILABLE"
	CodeSyncBackendUnavailable   ErrorCode = "SYNC_BACKEND_UNAVAILABLE"
)

// HTTPStatus returns the HTTP status paired with the code in api-contracts §1.3.
func (c ErrorCode) HTTPStatus() int {
	switch c {
	case CodeValidationErr:
		return 400
	case CodeUnauthenticated:
		return 401
	case CodeSpaceAccessDenied, CodeSpaceWriteDenied, CodeSpacePromoteDenied,
		CodeSpaceAdminRequired, CodePurgeForbidden, CodeLifecycleScopeForbidden:
		return 403
	case CodeMemoryNotFound, CodeSessionNotFound, CodeSpaceNotFound,
		CodeRecallNotFound, CodeProposalNotFound:
		return 404
	case CodeVersionConflict, CodeSessionAlreadyEnded, CodeProposalAlreadyResolved,
		CodeSpaceSyncInProgress, CodeSpaceDiverged,
		CodeSlotBudgetExceeded, CodeTokenBudgetExceeded:
		return 409
	case CodeMemoryExpired, CodeMemorySuperseded:
		return 410
	case CodeUnsupportedMediaType:
		return 415
	case CodeLinkIntegrity, CodeSpacePolicyInvalid,
		CodePromotionNotConsolidated, CodeRecallBudgetInvalid:
		return 422
	case CodeRateLimited:
		return 429
	case CodeStoreUnavailable, CodeSyncBackendUnavailable:
		return 503
	default:
		return 500
	}
}

// Error is the structured domain error. It implements error and carries the
// stable code, a log-safe message, and optional structured details.
type Error struct {
	Code    ErrorCode      `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

// Error implements the error interface.
func (e *Error) Error() string {
	if len(e.Details) == 0 {
		return string(e.Code) + ": " + e.Message
	}
	return fmt.Sprintf("%s: %s (%v)", e.Code, e.Message, e.Details)
}

// GetCode returns the stable API error code. (A method named Code would
// collide with the Code field.)
func (e *Error) GetCode() ErrorCode { return e.Code }

// WithDetail returns a shallow copy with one detail entry added.
func (e *Error) WithDetail(key string, value any) *Error {
	d := make(map[string]any, len(e.Details)+1)
	for k, v := range e.Details {
		d[k] = v
	}
	d[key] = value
	return &Error{Code: e.Code, Message: e.Message, Details: d}
}

// Sentinel errors for errors.Is matching across layers.
var (
	ErrNotFound            = &Error{Code: CodeMemoryNotFound, Message: "resource not found"}
	ErrValidation          = &Error{Code: CodeValidationErr, Message: "validation failed"}
	ErrVersionConflict     = &Error{Code: CodeVersionConflict, Message: "version conflict"}
	ErrSlotBudgetExceeded  = &Error{Code: CodeSlotBudgetExceeded, Message: "instruction slot budget exceeded"}
	ErrTokenBudgetExceeded = &Error{Code: CodeTokenBudgetExceeded, Message: "token budget exceeded"}
	ErrSpaceAccessDenied   = &Error{Code: CodeSpaceAccessDenied, Message: "principal cannot read the space"}
	ErrDuplicateProposal   = &Error{Code: CodeProposalAlreadyResolved, Message: "proposal already resolved"}
	ErrMemoryDeleted       = &Error{Code: CodeMemorySuperseded, Message: "memory already closed"}
	ErrInvalidState        = &Error{Code: CodeInternal, Message: "invalid state transition"}
)

// NewValidationError builds a VALIDATION_ERROR with field context.
func NewValidationError(field, got, want string) *Error {
	return &Error{
		Code:    CodeValidationErr,
		Message: fmt.Sprintf("%s: invalid value", field),
		Details: map[string]any{"field": field, "got": got, "expected": want},
	}
}

// FromError extracts a *Error from err, wrapping unknown errors as INTERNAL.
func FromError(err error) *Error {
	if err == nil {
		return nil
	}
	var de *Error
	if errors.As(err, &de) {
		return de
	}
	return &Error{Code: CodeInternal, Message: err.Error()}
}

// ErrorResponse is the standard API error envelope (api-contracts §1.1).
// Every non-2xx REST response body and MCP tool error uses this shape.
type ErrorResponse struct {
	Error struct {
		Code    ErrorCode      `json:"code"`
		Message string         `json:"message"`
		Details map[string]any `json:"details,omitempty"`
	} `json:"error"`
	RequestID string `json:"request_id,omitempty"`
	DocURL    string `json:"doc_url,omitempty"`
}

// NewErrorResponse builds the envelope for a domain error; requestID echoes
// X-Request-Id or is server-generated, docURL is a versioned doc anchor.
func NewErrorResponse(err error, requestID string) ErrorResponse {
	de := FromError(err)
	var body ErrorResponse
	body.Error.Code = de.Code
	body.Error.Message = de.Message
	body.Error.Details = de.Details
	body.RequestID = requestID
	body.DocURL = "https://memory.example.org/docs/errors/" + string(de.Code)
	return body
}

// intString / floatString are small formatting helpers shared by validators.
func intString(i int) string       { return fmt.Sprint(i) }
func floatString(f float64) string { return fmt.Sprint(f) }
