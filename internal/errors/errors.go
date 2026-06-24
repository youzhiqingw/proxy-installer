package errors

import (
	stderrors "errors"
	"fmt"
)

// ErrorCode categorizes errors for frontend and logging discrimination.
type ErrorCode string

const (
	CodeDeploy    ErrorCode = "DEPLOY"
	CodeSSH       ErrorCode = "SSH"
	CodeSpeedTest ErrorCode = "SPEED"
	CodeIPQuality ErrorCode = "IP_QUALITY"
	CodeValidation ErrorCode = "VALIDATION"
	CodeInternal  ErrorCode = "INTERNAL"
)

// AppError is the base structured error type.
type AppError struct {
	Code    ErrorCode
	Message string
	Cause   error // wrapped original error (may be nil)
}

func (e *AppError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

func (e *AppError) Unwrap() error {
	return e.Cause
}

// Is implements errors.Is for error code matching.
func (e *AppError) Is(target error) bool {
	if t, ok := target.(*AppError); ok {
		return e.Code == t.Code
	}
	return false
}

// NewDeploy creates a new AppError with CodeDeploy.
func NewDeploy(msg string, cause error) *AppError {
	return &AppError{Code: CodeDeploy, Message: msg, Cause: cause}
}

// NewSSH creates a new AppError with CodeSSH.
func NewSSH(msg string, cause error) *AppError {
	return &AppError{Code: CodeSSH, Message: msg, Cause: cause}
}

// NewSpeedTest creates a new AppError with CodeSpeedTest.
func NewSpeedTest(msg string, cause error) *AppError {
	return &AppError{Code: CodeSpeedTest, Message: msg, Cause: cause}
}

// NewIPQuality creates a new AppError with CodeIPQuality.
func NewIPQuality(msg string, cause error) *AppError {
	return &AppError{Code: CodeIPQuality, Message: msg, Cause: cause}
}

// NewValidation creates a new AppError with CodeValidation.
func NewValidation(msg string) *AppError {
	return &AppError{Code: CodeValidation, Message: msg}
}

// NewInternal creates a new AppError with CodeInternal.
func NewInternal(msg string, cause error) *AppError {
	return &AppError{Code: CodeInternal, Message: msg, Cause: cause}
}

// IsCode checks if an error has a specific error code.
func IsCode(err error, code ErrorCode) bool {
	var appErr *AppError
	if stderrors.As(err, &appErr) {
		return appErr.Code == code
	}
	return false
}

// GetCode extracts the error code from an error, returns CodeInternal if not an AppError.
func GetCode(err error) ErrorCode {
	var appErr *AppError
	if stderrors.As(err, &appErr) {
		return appErr.Code
	}
	return CodeInternal
}
