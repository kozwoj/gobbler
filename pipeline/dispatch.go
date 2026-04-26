package pipeline

import (
	"sync/atomic"
)

var routing atomic.Pointer[RoutingTable]

func init() {
	empty := make(RoutingTable)
	routing.Store(&empty)
}

// LookupType returns the TypeDescriptor for the given ItemType, or nil if not registered.
func LookupType(t ItemType) *TypeDescriptor {
	table := routing.Load()
	return (*table)[t]
}

// AddItemType registers a TypeDescriptor for a new ItemType in the routing table.
// The swap is atomic so no reader ever sees a partial update.
func AddItemType(t ItemType, desc *TypeDescriptor) {
	old := routing.Load()
	newTable := make(RoutingTable, len(*old)+1)
	for k, v := range *old {
		newTable[k] = v
	}
	newTable[t] = desc
	routing.Store(&newTable)
}

// RemoveItemType removes the TypeDescriptor for the given ItemType from the routing table.
// The swap is atomic so the dispatcher never sees a partial update.
// Call this from the REST definition endpoint before stopping the writer.
func RemoveItemType(t ItemType) {
	old := routing.Load()
	newTable := make(RoutingTable, len(*old))
	for k, v := range *old {
		if k != t {
			newTable[k] = v
		}
	}
	routing.Store(&newTable)
}

// Reset clears all pipeline state after the pipeline has been fully stopped.
// It must only be called after wg.Wait() has returned, guaranteeing that the
// dispatcher and all worker goroutines have exited.
// After Reset the pipeline can be reconfigured and started again with Start.
func Reset() {
	empty := make(RoutingTable)
	routing.Store(&empty)
}
