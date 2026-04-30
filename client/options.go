package gobblerclient

import "time"

// Option is a functional option for configuring a Client.
type Option func(*config)

// config holds the resolved configuration for a realClient.
type config struct {
	types         map[string]struct{}
	batchSize     int
	flushInterval time.Duration
}

// WithTypes registers the type names the client should log and are understood by the Gobbler server.
// Log calls with any other type name will return an error immediately.
func WithTypes(names ...string) Option {
	return func(c *config) {
		for _, n := range names {
			c.types[n] = struct{}{}
		}
	}
}

// WithBatchSize sets the number of buffered items that triggers an automatic flush.
// Default: 100.
func WithBatchSize(n int) Option {
	return func(c *config) {
		c.batchSize = n
	}
}

// WithFlushInterval sets how often the background goroutine flushes the buffer.
// Default: 10 seconds.
func WithFlushInterval(d time.Duration) Option {
	return func(c *config) {
		c.flushInterval = d
	}
}

// defaultConfig returns a config populated with default values.
func defaultConfig() config {
	return config{
		types:         make(map[string]struct{}),
		batchSize:     100,
		flushInterval: 10 * time.Second,
	}
}
