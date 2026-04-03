package errors

import (
	"errors"
	"fmt"
	"testing"
)

func TestSentinelErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		msg  string
	}{
		{"ErrNotFound", ErrNotFound, "not found"},
		{"ErrTimeout", ErrTimeout, "timeout"},
		{"ErrClosed", ErrClosed, "closed"},
		{"ErrUnauthorized", ErrUnauthorized, "unauthorized"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.Error() != tt.msg {
				t.Errorf("got %q, want %q", tt.err.Error(), tt.msg)
			}
		})
	}
}

func TestConnectError(t *testing.T) {
	cause := fmt.Errorf("connection refused")
	err := &ConnectError{Address: "127.0.0.1:8080", Cause: cause}

	if err.Error() != "connect 127.0.0.1:8080: connection refused" {
		t.Errorf("unexpected Error(): %s", err.Error())
	}

	if !errors.Is(err, cause) {
		t.Error("Unwrap should return cause")
	}

	var ce *ConnectError
	if !errors.As(err, &ce) {
		t.Error("errors.As should match ConnectError")
	}
	if ce.Address != "127.0.0.1:8080" {
		t.Error("Address mismatch")
	}
}

func TestTimeoutError(t *testing.T) {
	t.Run("with cause", func(t *testing.T) {
		cause := fmt.Errorf("dial timeout")
		err := &TimeoutError{Op: "connect", Cause: cause}

		if err.Error() != "connect timeout: dial timeout" {
			t.Errorf("unexpected Error(): %s", err.Error())
		}
	})

	t.Run("without cause", func(t *testing.T) {
		err := &TimeoutError{Op: "read"}

		if err.Error() != "read timeout" {
			t.Errorf("unexpected Error(): %s", err.Error())
		}
	})

	t.Run("Is ErrTimeout", func(t *testing.T) {
		err := &TimeoutError{Op: "write"}

		if !errors.Is(err, ErrTimeout) {
			t.Error("TimeoutError should match ErrTimeout via Is")
		}
	})

	t.Run("Unwrap returns ErrTimeout", func(t *testing.T) {
		err := &TimeoutError{Op: "send", Cause: fmt.Errorf("something")}

		if err.Unwrap() != ErrTimeout {
			t.Error("Unwrap should return ErrTimeout")
		}
	})

	t.Run("wrap chain", func(t *testing.T) {
		inner := &TimeoutError{Op: "inner"}
		outer := fmt.Errorf("outer: %w", inner)

		if !errors.Is(outer, ErrTimeout) {
			t.Error("wrapped TimeoutError should match ErrTimeout")
		}
	})
}

func TestAuthError(t *testing.T) {
	err := &AuthError{Reason: "invalid token"}

	if err.Error() != "auth failed: invalid token" {
		t.Errorf("unexpected Error(): %s", err.Error())
	}

	if !errors.Is(err, ErrUnauthorized) {
		t.Error("AuthError should match ErrUnauthorized via Is")
	}

	var ae *AuthError
	if !errors.As(err, &ae) {
		t.Error("errors.As should match AuthError")
	}
}

func TestClusterError(t *testing.T) {
	cause := fmt.Errorf("node unreachable")

	t.Run("with node", func(t *testing.T) {
		err := &ClusterError{Op: "join", Node: "node-1", Cause: cause}

		if err.Error() != "cluster join [node-1]: node unreachable" {
			t.Errorf("unexpected Error(): %s", err.Error())
		}

		if !errors.Is(err, cause) {
			t.Error("Unwrap should return cause")
		}
	})

	t.Run("without node", func(t *testing.T) {
		err := &ClusterError{Op: "join", Cause: cause}

		if err.Error() != "cluster join: node unreachable" {
			t.Errorf("unexpected Error(): %s", err.Error())
		}
	})

	t.Run("As ClusterError", func(t *testing.T) {
		err := &ClusterError{Op: "leave", Node: "node-2", Cause: cause}
		wrapped := fmt.Errorf("failed: %w", err)

		var ce *ClusterError
		if !errors.As(wrapped, &ce) {
			t.Error("errors.As should match ClusterError")
		}
		if ce.Op != "leave" || ce.Node != "node-2" {
			t.Error("field mismatch")
		}
	})
}

func TestCodecError(t *testing.T) {
	cause := fmt.Errorf("invalid json")

	t.Run("with type name", func(t *testing.T) {
		err := &CodecError{Op: "decode", TypeName: "MyMsg", Cause: cause}

		if err.Error() != "codec decode [MyMsg]: invalid json" {
			t.Errorf("unexpected Error(): %s", err.Error())
		}
	})

	t.Run("without type name", func(t *testing.T) {
		err := &CodecError{Op: "encode", Cause: cause}

		if err.Error() != "codec encode: invalid json" {
			t.Errorf("unexpected Error(): %s", err.Error())
		}
	})

	t.Run("Unwrap", func(t *testing.T) {
		err := &CodecError{Op: "decode", Cause: cause}

		if !errors.Is(err, cause) {
			t.Error("Unwrap should return cause")
		}
	})
}
