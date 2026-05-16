package apperr

// AppError represents a domain-level error with an HTTP status code,
// a machine-readable code, and a human-readable message.
type AppError struct {
	Code       string
	Message    string
	HTTPStatus int
}

// Error implements the error interface.
func (e *AppError) Error() string {
	return e.Message
}

// New clones a sentinel AppError with a custom message.
// Always returns a new pointer — never mutates the original sentinel.
func New(base *AppError, message string) *AppError {
	return &AppError{
		Code:       base.Code,
		Message:    message,
		HTTPStatus: base.HTTPStatus,
	}
}

// Sentinel errors — use New() to attach a specific message.
//
// Example:
//   return apperr.New(apperr.ErrNotFound, "user not found")
//   return apperr.New(apperr.ErrInvalidInput, "email is required")
// or,
//	 return apperr.ErrInvalidInput
var (
	ErrNotFound = &AppError{
		Code:       "NOT_FOUND",
		Message:    "resource not found",
		HTTPStatus: 404,
	}

	ErrUnauthorized = &AppError{
		Code:       "UNAUTHORIZED",
		Message:    "authentication required",
		HTTPStatus: 401,
	}

	ErrForbidden = &AppError{
		Code:       "FORBIDDEN",
		Message:    "you do not have permission to perform this action",
		HTTPStatus: 403,
	}

	ErrInvalidInput = &AppError{
		Code:       "INVALID_INPUT",
		Message:    "invalid input",
		HTTPStatus: 400,
	}

	ErrInsufficientStock = &AppError{
		Code:       "INSUFFICIENT_STOCK",
		Message:    "not enough stock available",
		HTTPStatus: 400,
	}

	ErrConflict = &AppError{
		Code:       "CONFLICT",
		Message:    "resource already exists",
		HTTPStatus: 409,
	}

	ErrInternal = &AppError{
		Code:       "INTERNAL_ERROR",
		Message:    "something went wrong",
		HTTPStatus: 500,
	}
)
