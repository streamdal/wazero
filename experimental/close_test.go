package experimental_test

import (
	"context"
	"testing"

	"github.com/streamdal/wazero/experimental"
	"github.com/streamdal/wazero/internal/expctxkeys"
	"github.com/streamdal/wazero/internal/testing/require"
)

// testCtx is an arbitrary, non-default context. Non-nil also prevents linter errors.
var testCtx = context.WithValue(context.Background(), struct{}{}, "arbitrary")

func TestWithCloseNotifier(t *testing.T) {
	tests := []struct {
		name         string
		notification experimental.CloseNotifier
		expected     bool
	}{
		{
			name:     "returns input when notification nil",
			expected: false,
		},
		{
			name:         "decorates with notification",
			notification: experimental.CloseNotifyFunc(func(context.Context, uint32) {}),
			expected:     true,
		},
	}

	for _, tt := range tests {
		tc := tt
		t.Run(tc.name, func(t *testing.T) {
			if decorated := experimental.WithCloseNotifier(testCtx, tc.notification); tc.expected {
				require.NotNil(t, decorated.Value(expctxkeys.CloseNotifierKey{}))
			} else {
				require.Same(t, testCtx, decorated)
			}
		})
	}
}
