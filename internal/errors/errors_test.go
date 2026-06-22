package errors_test

import (
	"errors"
	"fmt"
	"testing"

	apperrors "proxy-installer/internal/errors"
)

func TestAppError_Error(t *testing.T) {
	t.Run("without cause", func(t *testing.T) {
		err := &apperrors.AppError{Code: apperrors.CodeDeploy, Message: "deploy failed"}
		got := err.Error()
		want := "[DEPLOY] deploy failed"
		if got != want {
			t.Errorf("Error() = %q, want %q", got, want)
		}
	})

	t.Run("with cause", func(t *testing.T) {
		cause := fmt.Errorf("connection refused")
		err := &apperrors.AppError{Code: apperrors.CodeSSH, Message: "ssh dial failed", Cause: cause}
		got := err.Error()
		want := "[SSH] ssh dial failed: connection refused"
		if got != want {
			t.Errorf("Error() = %q, want %q", got, want)
		}
	})

	t.Run("all error codes render correctly", func(t *testing.T) {
		codes := []apperrors.ErrorCode{
			apperrors.CodeDeploy,
			apperrors.CodeSSH,
			apperrors.CodeSpeedTest,
			apperrors.CodeIPQuality,
			apperrors.CodeValidation,
			apperrors.CodeInternal,
		}
		for _, code := range codes {
			err := &apperrors.AppError{Code: code, Message: "test"}
			got := err.Error()
			want := fmt.Sprintf("[%s] test", code)
			if got != want {
				t.Errorf("Error() for code %q = %q, want %q", code, got, want)
			}
		}
	})
}

func TestAppError_Unwrap(t *testing.T) {
	t.Run("returns cause when set", func(t *testing.T) {
		cause := fmt.Errorf("underlying io timeout")
		err := &apperrors.AppError{Code: apperrors.CodeSpeedTest, Message: "speed test failed", Cause: cause}
		if unwrapped := err.Unwrap(); unwrapped != cause {
			t.Errorf("Unwrap() = %v, want %v", unwrapped, cause)
		}
	})

	t.Run("returns nil when no cause", func(t *testing.T) {
		err := &apperrors.AppError{Code: apperrors.CodeValidation, Message: "invalid input"}
		if unwrapped := err.Unwrap(); unwrapped != nil {
			t.Errorf("Unwrap() = %v, want nil", unwrapped)
		}
	})

	t.Run("works with errors.As through wrapping chain", func(t *testing.T) {
		cause := fmt.Errorf("disk full")
		inner := &apperrors.AppError{Code: apperrors.CodeDeploy, Message: "write failed", Cause: cause}
		outer := fmt.Errorf("deploy step 3: %w", inner)

		var appErr *apperrors.AppError
		if !errors.As(outer, &appErr) {
			t.Fatal("errors.As should find AppError through wrapping chain")
		}
		if appErr.Code != apperrors.CodeDeploy {
			t.Errorf("Code = %q, want %q", appErr.Code, apperrors.CodeDeploy)
		}
	})
}

func TestAppError_Is(t *testing.T) {
	t.Run("matches same code", func(t *testing.T) {
		err1 := apperrors.NewSSH("first error", nil)
		err2 := apperrors.NewSSH("different message", fmt.Errorf("some cause"))
		if !errors.Is(err1, err2) {
			t.Error("errors.Is should return true for same error code")
		}
	})

	t.Run("does not match different code", func(t *testing.T) {
		sshErr := apperrors.NewSSH("ssh error", nil)
		deployErr := apperrors.NewDeploy("deploy error", nil)
		if errors.Is(sshErr, deployErr) {
			t.Error("errors.Is should return false for different error codes")
		}
	})

	t.Run("does not match non-AppError", func(t *testing.T) {
		appErr := apperrors.NewInternal("internal", nil)
		plainErr := fmt.Errorf("plain error")
		if errors.Is(appErr, plainErr) {
			t.Error("errors.Is should return false when target is not an AppError")
		}
	})

	t.Run("works through fmt.Errorf wrapping", func(t *testing.T) {
		inner := apperrors.NewSpeedTest("timeout", nil)
		wrapped := fmt.Errorf("node 10.0.0.1: %w", inner)
		target := &apperrors.AppError{Code: apperrors.CodeSpeedTest}
		if !errors.Is(wrapped, target) {
			t.Error("errors.Is should match AppError code through fmt.Errorf wrapping")
		}
	})
}

func TestConstructors(t *testing.T) {
	tests := []struct {
		name     string
		err      *apperrors.AppError
		wantCode apperrors.ErrorCode
		wantMsg  string
		hasCause bool
	}{
		{
			name:     "NewDeploy",
			err:      apperrors.NewDeploy("deploy failed", fmt.Errorf("timeout")),
			wantCode: apperrors.CodeDeploy,
			wantMsg:  "deploy failed",
			hasCause: true,
		},
		{
			name:     "NewSSH",
			err:      apperrors.NewSSH("ssh dial error", fmt.Errorf("refused")),
			wantCode: apperrors.CodeSSH,
			wantMsg:  "ssh dial error",
			hasCause: true,
		},
		{
			name:     "NewSpeedTest",
			err:      apperrors.NewSpeedTest("speed test timeout", fmt.Errorf("deadline exceeded")),
			wantCode: apperrors.CodeSpeedTest,
			wantMsg:  "speed test timeout",
			hasCause: true,
		},
		{
			name:     "NewIPQuality",
			err:      apperrors.NewIPQuality("ip blocked", fmt.Errorf("blacklisted")),
			wantCode: apperrors.CodeIPQuality,
			wantMsg:  "ip blocked",
			hasCause: true,
		},
		{
			name:     "NewValidation",
			err:      apperrors.NewValidation("invalid host format"),
			wantCode: apperrors.CodeValidation,
			wantMsg:  "invalid host format",
			hasCause: false,
		},
		{
			name:     "NewInternal",
			err:      apperrors.NewInternal("unexpected state", fmt.Errorf("nil pointer")),
			wantCode: apperrors.CodeInternal,
			wantMsg:  "unexpected state",
			hasCause: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.err.Code != tc.wantCode {
				t.Errorf("Code = %q, want %q", tc.err.Code, tc.wantCode)
			}
			if tc.err.Message != tc.wantMsg {
				t.Errorf("Message = %q, want %q", tc.err.Message, tc.wantMsg)
			}
			if tc.hasCause && tc.err.Cause == nil {
				t.Error("Cause should not be nil")
			}
			if !tc.hasCause && tc.err.Cause != nil {
				t.Errorf("Cause should be nil, got %v", tc.err.Cause)
			}
		})
	}
}

func TestIsCode(t *testing.T) {
	t.Run("returns true for matching AppError code", func(t *testing.T) {
		err := apperrors.NewSSH("auth failed", nil)
		if !apperrors.IsCode(err, apperrors.CodeSSH) {
			t.Error("IsCode should return true for matching code")
		}
	})

	t.Run("returns false for non-matching AppError code", func(t *testing.T) {
		err := apperrors.NewSSH("auth failed", nil)
		if apperrors.IsCode(err, apperrors.CodeDeploy) {
			t.Error("IsCode should return false for non-matching code")
		}
	})

	t.Run("returns false for plain error", func(t *testing.T) {
		err := fmt.Errorf("plain error")
		if apperrors.IsCode(err, apperrors.CodeInternal) {
			t.Error("IsCode should return false for non-AppError")
		}
	})

	t.Run("returns false for nil error", func(t *testing.T) {
		if apperrors.IsCode(nil, apperrors.CodeInternal) {
			t.Error("IsCode should return false for nil error")
		}
	})

	t.Run("works through wrapped AppError", func(t *testing.T) {
		inner := apperrors.NewIPQuality("score too low", nil)
		wrapped := fmt.Errorf("node check: %w", inner)
		if !apperrors.IsCode(wrapped, apperrors.CodeIPQuality) {
			t.Error("IsCode should find code through wrapping chain")
		}
	})
}

func TestGetCode(t *testing.T) {
	t.Run("returns correct code for AppError", func(t *testing.T) {
		tests := []struct {
			name     string
			err      error
			wantCode apperrors.ErrorCode
		}{
			{"deploy", apperrors.NewDeploy("fail", nil), apperrors.CodeDeploy},
			{"ssh", apperrors.NewSSH("fail", nil), apperrors.CodeSSH},
			{"speedtest", apperrors.NewSpeedTest("fail", nil), apperrors.CodeSpeedTest},
			{"ipquality", apperrors.NewIPQuality("fail", nil), apperrors.CodeIPQuality},
			{"validation", apperrors.NewValidation("fail"), apperrors.CodeValidation},
			{"internal", apperrors.NewInternal("fail", nil), apperrors.CodeInternal},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				got := apperrors.GetCode(tc.err)
				if got != tc.wantCode {
					t.Errorf("GetCode() = %q, want %q", got, tc.wantCode)
				}
			})
		}
	})

	t.Run("returns CodeInternal for plain error", func(t *testing.T) {
		err := fmt.Errorf("some random error")
		got := apperrors.GetCode(err)
		if got != apperrors.CodeInternal {
			t.Errorf("GetCode() = %q, want %q", got, apperrors.CodeInternal)
		}
	})

	t.Run("returns CodeInternal for nil error", func(t *testing.T) {
		got := apperrors.GetCode(nil)
		if got != apperrors.CodeInternal {
			t.Errorf("GetCode(nil) = %q, want %q", got, apperrors.CodeInternal)
		}
	})

	t.Run("extracts code through wrapping chain", func(t *testing.T) {
		inner := apperrors.NewSpeedTest("latency high", nil)
		wrapped := fmt.Errorf("node 10.0.0.1: %w", inner)
		got := apperrors.GetCode(wrapped)
		if got != apperrors.CodeSpeedTest {
			t.Errorf("GetCode() through wrapping = %q, want %q", got, apperrors.CodeSpeedTest)
		}
	})
}
