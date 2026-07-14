// Package lifecycle ties process shutdown signals to context cancellation.
package lifecycle

import (
	"context"
	"os/signal"
)

func SignalContext(parent context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(parent, shutdownSignals()...)
}
