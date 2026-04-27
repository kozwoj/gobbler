// blobtest is a standalone diagnostic tool that verifies Azure Blob Storage
// access using the credentials in tester/secrets.json.
//
// Run from the repo root:
//
//	go run ./tester/blobtest
//
// Each step is printed individually so failures are easy to pinpoint.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/appendblob"
)

type secrets struct {
	AccountName string `json:"accountName"`
	AccountKey  string `json:"accountKey"`
}

const testContainer = "gobbler-blobtest"

// readSeekCloser wraps strings.Reader to satisfy io.ReadSeekCloser,
// which is required by the appendblob AppendBlock API.
type readSeekCloser struct {
	*strings.Reader
}

func (r readSeekCloser) Close() error { return nil }

func main() {
	ok := run()
	if !ok {
		os.Exit(1)
	}
}

func run() bool {
	// ── Step 1: read secrets ──────────────────────────────────────────────────
	fmt.Print("Step 1: reading tester/secrets.json ... ")
	data, err := os.ReadFile("tester/secrets.json")
	if err != nil {
		fmt.Printf("FAIL\n  %v\n", err)
		return false
	}
	var s secrets
	if err := json.Unmarshal(data, &s); err != nil {
		fmt.Printf("FAIL\n  %v\n", err)
		return false
	}
	fmt.Printf("OK  (account: %s)\n", s.AccountName)

	// ── Step 2: create shared-key credential ─────────────────────────────────
	fmt.Print("Step 2: creating shared-key credential ... ")
	cred, err := azblob.NewSharedKeyCredential(s.AccountName, s.AccountKey)
	if err != nil {
		fmt.Printf("FAIL\n  %v\n", err)
		return false
	}
	fmt.Println("OK")

	// ── Step 3: create service client ────────────────────────────────────────
	fmt.Print("Step 3: creating blob service client ... ")
	serviceURL := fmt.Sprintf("https://%s.blob.core.windows.net/", s.AccountName)
	serviceClient, err := azblob.NewClientWithSharedKeyCredential(serviceURL, cred, nil)
	if err != nil {
		fmt.Printf("FAIL\n  %v\n", err)
		return false
	}
	fmt.Println("OK")

	ctx := context.Background()

	// ── Step 4: create test container ────────────────────────────────────────
	fmt.Printf("Step 4: creating container %q ... ", testContainer)
	containerClient := serviceClient.ServiceClient().NewContainerClient(testContainer)
	_, err = containerClient.Create(ctx, nil)
	if err != nil && !strings.Contains(err.Error(), "ContainerAlreadyExists") {
		fmt.Printf("FAIL\n  %v\n", err)
		return false
	}
	fmt.Println("OK")

	// ── Step 5: create an append blob ────────────────────────────────────────
	blobName := fmt.Sprintf("test_%s.csv", time.Now().UTC().Format("20060102_150405"))
	fmt.Printf("Step 5: creating append blob %q ... ", blobName)
	blobURL := fmt.Sprintf("https://%s.blob.core.windows.net/%s/%s", s.AccountName, testContainer, blobName)
	blobClient, err := appendblob.NewClientWithSharedKeyCredential(blobURL, cred, nil)
	if err != nil {
		fmt.Printf("FAIL\n  %v\n", err)
		return false
	}
	_, err = blobClient.Create(ctx, nil)
	if err != nil {
		fmt.Printf("FAIL\n  %v\n", err)
		return false
	}
	fmt.Println("OK")

	// ── Step 6: append a block ───────────────────────────────────────────────
	fmt.Print("Step 6: appending a CSV line ... ")
	line := "2026-04-26 12:00:00.000,hello,world\n"
	_, err = blobClient.AppendBlock(ctx, readSeekCloser{strings.NewReader(line)}, nil)
	if err != nil {
		fmt.Printf("FAIL\n  %v\n", err)
		return false
	}
	fmt.Println("OK")

	// ── Step 7: list blobs in container ──────────────────────────────────────
	fmt.Printf("Step 7: listing blobs in %q ... ", testContainer)
	pager := containerClient.NewListBlobsFlatPager(nil)
	count := 0
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			fmt.Printf("FAIL\n  %v\n", err)
			return false
		}
		for _, b := range page.Segment.BlobItems {
			fmt.Printf("\n  found: %s", *b.Name)
			count++
		}
	}
	fmt.Printf("\n  total: %d blob(s) — OK\n", count)

	fmt.Println("\nAll steps passed. Azure Blob Storage access is working.")
	return true
}
