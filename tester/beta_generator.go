package tester

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"time"
)

// BetaItem represents an item of type "beta" based on testDefinitions.json
type BetaItem struct {
	BetaStr  string  `json:"betaStr"`
	BetaBool bool    `json:"betaBool"`
	BetaReal float64 `json:"betaReal"`
}

// BetaGenerator generates random beta items
type BetaGenerator struct {
	rand *rand.Rand
}

// NewBetaGenerator creates a new beta item generator
func NewBetaGenerator() *BetaGenerator {
	return &BetaGenerator{
		rand: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// GenerateItem creates a single random beta item
func (g *BetaGenerator) GenerateItem() BetaItem {
	// Generate random string (3-8 characters)
	stringLength := g.rand.Intn(6) + 3
	str := g.generateRandomString(stringLength)

	// Generate random bool
	randomBool := g.rand.Intn(2) == 1

	// Generate random real (0.0-1000.0 with 2 decimal places)
	randomReal := g.rand.Float64() * 1000.0
	randomReal = float64(int(randomReal*100)) / 100 // Round to 2 decimal places

	return BetaItem{
		BetaStr:  str,
		BetaBool: randomBool,
		BetaReal: randomReal,
	}
}

// GenerateItems creates multiple random beta items
func (g *BetaGenerator) GenerateItems(count int) []BetaItem {
	items := make([]BetaItem, count)
	for i := 0; i < count; i++ {
		items[i] = g.GenerateItem()
	}
	return items
}

// GenerateJSON creates a single beta item as JSON string
func (g *BetaGenerator) GenerateJSON() (string, error) {
	item := g.GenerateItem()
	jsonData, err := json.Marshal(item)
	if err != nil {
		return "", fmt.Errorf("failed to marshal beta item to JSON: %v", err)
	}
	return string(jsonData), nil
}

// GenerateJSONArray creates multiple beta items as JSON array string
// Each item is wrapped in an object with the type name as key: [{"beta": {item1}}, {"beta": {item2}}, ...]
func (g *BetaGenerator) GenerateJSONArray(count int) (string, error) {
	items := g.GenerateItems(count)

	// Wrap each item in an object with "beta" as the key
	wrappedItems := make([]map[string]BetaItem, count)
	for i, item := range items {
		wrappedItems[i] = map[string]BetaItem{"beta": item}
	}

	jsonData, err := json.Marshal(wrappedItems)
	if err != nil {
		return "", fmt.Errorf("failed to marshal beta items to JSON: %v", err)
	}
	return string(jsonData), nil
}

// NewBetaGeneratorWithRand creates a BetaGenerator backed by the provided rand.Rand.
func NewBetaGeneratorWithRand(r *rand.Rand) *BetaGenerator {
	return &BetaGenerator{rand: r}
}

// TypeName implements ItemGenerator.
func (g *BetaGenerator) TypeName() string { return "beta" }

// GenerateWrapped implements ItemGenerator.
func (g *BetaGenerator) GenerateWrapped() map[string]any {
	item := g.GenerateItem()
	return map[string]any{
		"beta": map[string]any{
			"betaStr":  item.BetaStr,
			"betaBool": item.BetaBool,
			"betaReal": item.BetaReal,
		},
	}
}

// generateRandomString creates a random string of specified length
func (g *BetaGenerator) generateRandomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	result := make([]byte, length)
	for i := range result {
		result[i] = charset[g.rand.Intn(len(charset))]
	}
	return string(result)
}

// PrintSampleItems prints sample beta items to console
func (g *BetaGenerator) PrintSampleItems(count int) {
	fmt.Printf("Sample Beta Items (%d):\n", count)
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
