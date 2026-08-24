package port

import "context"

// TransactionManager wraps multi-step operations in a single database
// transaction (unit of work). The implementation binds the tx to the context
// it hands to fn; repositories invoked inside fn must participate in that tx.
// An error returned by fn rolls back; a nil return commits. Nesting is the
// implementation's concern (typically join-existing-tx).
type TransactionManager interface {
	WithTx(ctx context.Context, fn func(ctx context.Context) error) error
}
