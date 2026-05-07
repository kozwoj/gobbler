package tester

import (
	"fmt"
	"math/rand"
)

// ItemGenerator is implemented by all item type generators.
type ItemGenerator interface {
	// TypeName returns the Gobbler item type name (e.g. "alpha").
	TypeName() string
	// GenerateWrapped returns one item in the Gobbler ingest format:
	// {"typeName": {"field": value, ...}}
	GenerateWrapped() map[string]any
}

// NewGenerator creates an ItemGenerator for the named type, backed by r.
// Returns an error if typeName is not recognised.
func NewGenerator(typeName string, r *rand.Rand) (ItemGenerator, error) {
	switch typeName {
	case "alpha":
		return NewAlphaGeneratorWithRand(r), nil
	case "beta":
		return NewBetaGeneratorWithRand(r), nil
	case "gamma":
		return NewGammaGeneratorWithRand(r), nil
	case "allscalars":
		return NewAllScalarsGeneratorWithRand(r), nil
	case "somescalars":
		return NewSomeScalarsGeneratorWithRand(r), nil
	case "vm-shutdown":
		return NewVmShutdownGeneratorWithRand(r), nil
	case "vm-start":
		return NewVmStartGeneratorWithRand(r), nil
	case "vm-reboot":
		return NewVmRebootGeneratorWithRand(r), nil
	default:
		return nil, fmt.Errorf("unknown item type %q; known types: alpha, beta, gamma, allscalars, somescalars, vm-shutdown, vm-start, vm-reboot", typeName)
	}
}
