package tester

import (
	"math/rand"
	"time"
)

// globalRand returns a new rand.Rand seeded from the current time.
// Used by standalone (non-shared-RNG) constructors.
func globalRand() *rand.Rand {
	return rand.New(rand.NewSource(time.Now().UnixNano()))
}

// randString returns a random alphanumeric string with length in [minLen, maxLen].
func randString(r *rand.Rand, minLen, maxLen int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	n := minLen + r.Intn(maxLen-minLen+1)
	b := make([]byte, n)
	for i := range b {
		b[i] = charset[r.Intn(len(charset))]
	}
	return string(b)
}

// randDatetime returns a random datetime string (within the last 30 days) in
// the Gobbler datetime format "2006-01-02 15:04:05".
func randDatetime(r *rand.Rand) string {
	now := time.Now()
	offset := time.Duration(r.Intn(30*24*60)) * time.Minute
	return now.Add(-offset).Format("2006-01-02 15:04:05")
}

var timespanValues = []string{"1s", "30s", "5m", "1h", "1d"}
