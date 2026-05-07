package tester

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"time"
)

// GammaItem represents an item of type "gamma" based on testDefinitions.json
type GammaItem struct {
	GammaInt     int    `json:"gammaInt"`
	GammaStr     string `json:"gammaStr"`
	GammaDynamic string `json:"gammaDynamic"` // JSON representation of either alpha or beta item
}

// GammaGenerator generates random gamma items
type GammaGenerator struct {
	rand           *rand.Rand
	alphaGenerator *AlphaGenerator
	betaGenerator  *BetaGenerator
}

// NewGammaGenerator creates a new gamma item generator
func NewGammaGenerator() *GammaGenerator {
	return &GammaGenerator{
		rand:           rand.New(rand.NewSource(time.Now().UnixNano())),
		alphaGenerator: NewAlphaGenerator(),
		betaGenerator:  NewBetaGenerator(),
	}
}

// GenerateItem creates a single random gamma item
func (g *GammaGenerator) GenerateItem() GammaItem {
	// Generate random int (0-10000)
	randomInt := g.rand.Intn(10001)

	// Generate random string (4-12 characters)
	stringLength := g.rand.Intn(9) + 4
	str := g.generateRandomString(stringLength)

	// Generate dynamic field containing JSON of either alpha or beta item
	var dynamicJSON string
	var err error

	// Randomly choose between alpha and beta (50/50 chance)
	if g.rand.Intn(2) == 0 {
		// Generate alpha item JSON (just the item data, not wrapped)
		dynamicJSON, err = g.alphaGenerator.GenerateJSON()
		if err != nil {
			// Fallback to simple JSON if alpha generation fails
			dynamicJSON = `{"type":"alpha","error":"generation failed"}`
		}
	} else {
		// Generate beta item JSON (just the item data, not wrapped)
		dynamicJSON, err = g.betaGenerator.GenerateJSON()
		if err != nil {
			// Fallback to simple JSON if beta generation fails
			dynamicJSON = `{"type":"beta","error":"generation failed"}`
		}
	}

	return GammaItem{
		GammaInt:     randomInt,
		GammaStr:     str,
		GammaDynamic: dynamicJSON,
	}
}

// GenerateItems creates multiple random gamma items
func (g *GammaGenerator) GenerateItems(count int) []GammaItem {
	items := make([]GammaItem, count)
	for i := 0; i < count; i++ {
		items[i] = g.GenerateItem()
	}
	return items
}

// GenerateJSON creates a single gamma item as JSON string
func (g *GammaGenerator) GenerateJSON() (string, error) {
	item := g.GenerateItem()
	jsonData, err := json.Marshal(item)
	if err != nil {
		return "", fmt.Errorf("failed to marshal gamma item to JSON: %v", err)
	}
	return string(jsonData), nil
}

// GenerateJSONArray creates multiple gamma items as JSON array string
// Each item is wrapped in an object with the type name as key: [{"gammaItem": {item1}}, {"gammaItem": {item2}}, ...]
func (g *GammaGenerator) GenerateJSONArray(count int) (string, error) {
	items := g.GenerateItems(count)

	// Wrap each item in an object with "gammaItem" as the key (to match test expectations)
	wrappedItems := make([]map[string]GammaItem, count)
	for i, item := range items {
		wrappedItems[i] = map[string]GammaItem{"gamma": item}
	}

	jsonData, err := json.Marshal(wrappedItems)
	if err != nil {
		return "", fmt.Errorf("failed to marshal gamma items to JSON: %v", err)
	}
	return string(jsonData), nil
}

// NewGammaGeneratorWithRand creates a GammaGenerator backed by the provided rand.Rand.
func NewGammaGeneratorWithRand(r *rand.Rand) *GammaGenerator {
	return &GammaGenerator{
		rand:           r,
		alphaGenerator: NewAlphaGeneratorWithRand(r),
		betaGenerator:  NewBetaGeneratorWithRand(r),
	}
}

// TypeName implements ItemGenerator.
func (g *GammaGenerator) TypeName() string { return "gamma" }

// GenerateWrapped implements ItemGenerator.
func (g *GammaGenerator) GenerateWrapped() map[string]any {
	item := g.GenerateItem()
	return map[string]any{
		"gamma": map[string]any{
			"gammaInt":     item.GammaInt,
			"gammaStr":     item.GammaStr,
			"gammaDynamic": item.GammaDynamic,
		},
	}
}

// generateRandomString creates a random string of specified length
func (g *GammaGenerator) generateRandomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	result := make([]byte, length)
	for i := range result {
		result[i] = charset[g.rand.Intn(len(charset))]
	}
	return string(result)
}

// PrintSampleItems prints sample gamma items to console
func (g *GammaGenerator) PrintSampleItems(count int) {
	fmt.Printf("Sample Gamma Items (%d):\n", count)
	fmt.Println("=======================")

	for i := 0; i < count; i++ {
		jsonStr, err := g.GenerateJSON()
		if err != nil {
			fmt.Printf("Error generating item %d: %v\n", i+1, err)
			continue
		}
		fmt.Printf("%d: %s\n", i+1, jsonStr)
	}
}

// PrintSampleItemsPretty prints sample gamma items with pretty formatting
func (g *GammaGenerator) PrintSampleItemsPretty(count int) {
	fmt.Printf("Sample Gamma Items (Pretty Format) (%d):\n", count)
	fmt.Println("========================================")

	for i := 0; i < count; i++ {
		item := g.GenerateItem()
		fmt.Printf("\n--- Item %d ---\n", i+1)
		fmt.Printf("GammaInt: %d\n", item.GammaInt)
		fmt.Printf("GammaStr: %s\n", item.GammaStr)
		fmt.Printf("GammaDynamic: %s\n", item.GammaDynamic)
	}
}
