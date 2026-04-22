package pipeline

import (
	"context"
	"sync"
	"sync/atomic"
)

var (
	routing    atomic.Pointer[RoutingTable]
	inputQueue chan CSVitem
)

func init() {
	empty := make(RoutingTable)
	routing.Store(&empty)
}

// Start initialises the central input queue and launches the dispatcher goroutine.
// queueSize is the capacity of the central (multi-type) input channel.
// Call this once at application startup before accepting ingestion requests.
func Start(ctx context.Context, wg *sync.WaitGroup, queueSize int) {
	inputQueue = make(chan CSVitem, queueSize)

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case item := <-inputQueue:
				table := routing.Load()
				if desc := (*table)[item.Type]; desc != nil {
					select {
					case desc.Queue <- item:
						// routed
					default:
						// per-type queue full: drop (writer is the backpressure boundary)
					}
				}
				// unknown type: silently dropped (type should have been validated before Enqueue)
			}
		}
	}()
}

// Enqueue pushes a CSVitem onto the central input queue without blocking.
// Returns false when the queue is full; the caller (REST handler) should respond with 503.
func Enqueue(item CSVitem) bool {
	select {
	case inputQueue <- item:
		return true
	default:
		return false
	}
}

// LookupType returns the TypeDescriptor for the given ItemType, or nil if not registered.
// Use this in the REST ingest handler to reject unknown types before conversion.
func LookupType(t ItemType) *TypeDescriptor {
	table := routing.Load()
	return (*table)[t]
}

// AddItemType registers a TypeDescriptor for a new ItemType in the routing table.
// The swap is atomic so the dispatcher never sees a partial update.
// Call this from the REST definition endpoint after the writer has been started.
func AddItemType(t ItemType, desc *TypeDescriptor) {
	old := routing.Load()
	newTable := make(RoutingTable, len(*old)+1)
	for k, v := range *old {
		newTable[k] = v
	}
	newTable[t] = desc
	routing.Store(&newTable)
}
