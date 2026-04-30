// Package gobblerclient provides a client for sending log items to a Gobbler server.
package gobblerclient

// Client is the interface for sending log items to a Gobbler server.
// Both realClient and nopClient implement this interface.
type Client interface {
	// Log appends one item to the internal buffer. Returns an error immediately
	// if typeName was not registered at construction time, or if a threshold
	// flush triggered by this call fails.
	Log(typeName string, fields map[string]any) error

	// Flush sends all buffered items to the server now.
	Flush() error

	// Close flushes all remaining items and stops the background goroutine.
	// Close is idempotent.
	Close() error

	// SwapServer validates newURL as a Gobbler target (running + all registered
	// types present) and, if valid, atomically replaces the current endpoint.
	// On failure the current endpoint is kept unchanged.
	SwapServer(newURL string) error
}
