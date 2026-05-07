package tester

import "math/rand"

// AllScalarsGenerator generates random items for the "allscalars" type.
type AllScalarsGenerator struct {
	rand *rand.Rand
}

func NewAllScalarsGenerator() *AllScalarsGenerator {
	return NewAllScalarsGeneratorWithRand(globalRand())
}

func NewAllScalarsGeneratorWithRand(r *rand.Rand) *AllScalarsGenerator {
	return &AllScalarsGenerator{rand: r}
}

func (g *AllScalarsGenerator) TypeName() string { return "allscalars" }

func (g *AllScalarsGenerator) GenerateWrapped() map[string]any {
	item := map[string]any{
		"_boolean":  g.rand.Intn(2) == 1,
		"_datetime": randDatetime(g.rand),
		"_dynamic":  `{"key":"` + randString(g.rand, 4, 6) + `"}`,
		"_int":      g.rand.Intn(10001),
		"_real":     float64(int(g.rand.Float64()*100000)) / 100,
	}
	// optional fields: include ~70% of the time
	if g.rand.Float64() >= 0.3 {
		item["_string"] = randString(g.rand, 5, 10)
	}
	if g.rand.Float64() >= 0.3 {
		item["_timespan"] = timespanValues[g.rand.Intn(len(timespanValues))]
	}
	return map[string]any{"allscalars": item}
}

// SomeScalarsGenerator generates random items for the "somescalars" type.
type SomeScalarsGenerator struct {
	rand *rand.Rand
}

func NewSomeScalarsGenerator() *SomeScalarsGenerator {
	return NewSomeScalarsGeneratorWithRand(globalRand())
}

func NewSomeScalarsGeneratorWithRand(r *rand.Rand) *SomeScalarsGenerator {
	return &SomeScalarsGenerator{rand: r}
}

func (g *SomeScalarsGenerator) TypeName() string { return "somescalars" }

func (g *SomeScalarsGenerator) GenerateWrapped() map[string]any {
	return map[string]any{
		"somescalars": map[string]any{
			"_string":   randString(g.rand, 5, 10),
			"_boolean":  g.rand.Intn(2) == 1,
			"_datetime": randDatetime(g.rand),
			"_int":      g.rand.Intn(10001),
			"_real":     float64(int(g.rand.Float64()*100000)) / 100,
		},
	}
}
