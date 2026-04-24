package tester

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"time"
)

// AlphaItem represents an item of type "alpha" based on testDefinitions.json
type AlphaItem struct {
	AlphaStr  string `json:"alphaStr"`
	AlphaInt  int    `json:"alphaInt"`
	AlphaDate string `json:"alphaDate"` // GOBBLER datetime format: "2006-01-02 15:04:05" or "2006-01-02 15:04:05.000"
}

// AlphaGenerator generates random alpha items
type AlphaGenerator struct {
	rand *rand.Rand
}

// NewAlphaGenerator creates a new alpha item generator
func NewAlphaGenerator() *AlphaGenerator {
	return &AlphaGenerator{
		rand: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// GenerateItem creates a single random alpha item
func (g *AlphaGenerator) GenerateItem() AlphaItem {
	// Generate random string (5-10 characters)
	stringLength := g.rand.Intn(6) + 5
	str := g.generateRandomString(stringLength)

	// Generate random int (0-1000)
	randomInt := g.rand.Intn(1001)

	// Generate random datetime (within last 30 days) in GOBBLER format
	now := time.Now()
	daysBack := g.rand.Intn(30)
	hoursBack := g.rand.Intn(24)
	minutesBack := g.rand.Intn(60)
	randomDateTime := now.AddDate(0, 0, -daysBack).Add(-time.Duration(hoursBack) * time.Hour).Add(-time.Duration(minutesBack) * time.Minute)

	// Format datetime in GOBBLER format: "2006-01-02 15:04:05" (without milliseconds)
	// Randomly choose between format with or without milliseconds
	var dateTimeStr string
	if g.rand.Intn(2) == 0 {
		// Format without milliseconds: "2006-01-02 15:04:05"
		dateTimeStr = randomDateTime.Format("2006-01-02 15:04:05")
	} else {
		// Format with milliseconds: "2006-01-02 15:04:05.000"
		dateTimeStr = randomDateTime.Format("2006-01-02 15:04:05.000")
	}

	return AlphaItem{
		AlphaStr:  str,
		AlphaInt:  randomInt,
		AlphaDate: dateTimeStr,
	}
}

// GenerateItems creates multiple random alpha items
func (g *AlphaGenerator) GenerateItems(count int) []AlphaItem {
	items := make([]AlphaItem, count)
	for i := 0; i < count; i++ {
		items[i] = g.GenerateItem()
	}
	return items
}

// GenerateJSON creates a single alpha item as JSON string
func (g *AlphaGenerator) GenerateJSON() (string, error) {
	item := g.GenerateItem()
	jsonData, err := json.Marshal(item)
	if err != nil {
		return "", fmt.Errorf("failed to marshal alpha item to JSON: %v", err)
	}
	return string(jsonData), nil
}

// GenerateJSONArray creates multiple alpha items as JSON array string
// Each item is wrapped in an object with the type name as key: [{"alpha": {item1}}, {"alpha": {item2}}, ...]
func (g *AlphaGenerator) GenerateJSONArray(count int) (string, error) {
	items := g.GenerateItems(count)

	// Wrap each item in an object with "alpha" as the key
	wrappedItems := make([]map[string]AlphaItem, count)
	for i, item := range items {
		wrappedItems[i] = map[string]AlphaItem{"alpha": item}
	}

	jsonData, err := json.Marshal(wrappedItems)
	if err != nil {
		return "", fmt.Errorf("failed to marshal alpha items to JSON: %v", err)
	}
	return string(jsonData), nil
}

// generateRandomString creates a random string of specified length
func (g *AlphaGenerator) generateRandomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	result := make([]byte, length)
	for i := range result {
		result[i] = charset[g.rand.Intn(len(charset))]
	}
	return string(result)
}

// PrintSampleItems prints sample alpha items to console
func (g *AlphaGenerator) PrintSampleItems(count int) {
	fmt.Printf("Sample Alpha Items (%d):\n", count)
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
