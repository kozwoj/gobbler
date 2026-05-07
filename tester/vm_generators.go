package tester

import (
	"encoding/json"
	"math/rand"
)

var vmIDs = []string{
	"vm-001", "vm-002", "vm-003", "vm-004", "vm-005",
	"vm-006", "vm-007", "vm-008", "vm-009", "vm-010",
}

var shutdownReasons = []string{
	"planned maintenance", "user request", "power failure",
	"hardware failure", "unknown",
}

var rebootReasons = []string{
	"kernel update", "driver install", "scheduled maintenance",
	"crash recovery", "unknown",
}

var osProfiles = []map[string]string{
	{"os": "Windows", "version": "11"},
	{"os": "Windows", "version": "Server 2022"},
	{"os": "Linux", "version": "Ubuntu 22.04"},
	{"os": "Linux", "version": "RHEL 9"},
}

// VmShutdownGenerator generates random items for the "vm-shutdown" type.
type VmShutdownGenerator struct {
	rand *rand.Rand
}

func NewVmShutdownGenerator() *VmShutdownGenerator {
	return NewVmShutdownGeneratorWithRand(globalRand())
}

func NewVmShutdownGeneratorWithRand(r *rand.Rand) *VmShutdownGenerator {
	return &VmShutdownGenerator{rand: r}
}

func (g *VmShutdownGenerator) TypeName() string { return "vm-shutdown" }

func (g *VmShutdownGenerator) GenerateWrapped() map[string]any {
	return map[string]any{
		"vm-shutdown": map[string]any{
			"vmId":           vmIDs[g.rand.Intn(len(vmIDs))],
			"shutdownStart":  randDatetime(g.rand),
			"shutdownReason": shutdownReasons[g.rand.Intn(len(shutdownReasons))],
		},
	}
}

// VmStartGenerator generates random items for the "vm-start" type.
type VmStartGenerator struct {
	rand *rand.Rand
}

func NewVmStartGenerator() *VmStartGenerator {
	return NewVmStartGeneratorWithRand(globalRand())
}

func NewVmStartGeneratorWithRand(r *rand.Rand) *VmStartGenerator {
	return &VmStartGenerator{rand: r}
}

func (g *VmStartGenerator) TypeName() string { return "vm-start" }

func (g *VmStartGenerator) GenerateWrapped() map[string]any {
	return map[string]any{
		"vm-start": map[string]any{
			"vmId":      vmIDs[g.rand.Intn(len(vmIDs))],
			"startTime": randDatetime(g.rand),
		},
	}
}

// VmRebootGenerator generates random items for the "vm-reboot" type.
type VmRebootGenerator struct {
	rand *rand.Rand
}

func NewVmRebootGenerator() *VmRebootGenerator {
	return NewVmRebootGeneratorWithRand(globalRand())
}

func NewVmRebootGeneratorWithRand(r *rand.Rand) *VmRebootGenerator {
	return &VmRebootGenerator{rand: r}
}

func (g *VmRebootGenerator) TypeName() string { return "vm-reboot" }

func (g *VmRebootGenerator) GenerateWrapped() map[string]any {
	profile := osProfiles[g.rand.Intn(len(osProfiles))]
	osJSON, _ := json.Marshal(profile)

	item := map[string]any{
		"vmId":         vmIDs[g.rand.Intn(len(vmIDs))],
		"eventTime":    randDatetime(g.rand),
		"rebootStart":  randDatetime(g.rand),
		"rebootReason": rebootReasons[g.rand.Intn(len(rebootReasons))],
		"OS":           string(osJSON),
	}
	// rebootDuration is optional; include ~70% of the time
	if g.rand.Float64() >= 0.3 {
		item["rebootDuration"] = timespanValues[g.rand.Intn(len(timespanValues))]
	}
	return map[string]any{"vm-reboot": item}
}
