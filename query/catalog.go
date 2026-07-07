package query

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"
	"github.com/kozwoj/gobbler-query/query/catalog"
)

// schemaFile is the minimal structure needed to read a {typeName}.json file
// written by gobbler's FileWriter or BlobWriter.
type schemaFile struct {
	Name           string `json:"name"`
	OrderedColumns []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	} `json:"orderedColumns"`
}

// recognizedColumnTypes is the set of gobbler column type strings.
var recognizedColumnTypes = map[string]struct{}{
	"bool": {}, "datetime": {}, "dynamic": {},
	"int": {}, "real": {}, "string": {}, "timespan": {},
}

// validateSchema returns true if sf represents a valid gobbler item schema:
// non-empty name, at least one column, all column types recognised.
func validateSchema(sf schemaFile) bool {
	if sf.Name == "" || len(sf.OrderedColumns) == 0 {
		return false
	}
	for _, col := range sf.OrderedColumns {
		if _, ok := recognizedColumnTypes[col.Type]; !ok {
			return false
		}
	}
	return true
}

// BuildFileCatalog scans outputDir for type subdirectories and builds a
// catalog.Catalog from the {typeName}.json schema file found in each.
// Subdirectories that contain no *.json file are silently skipped — they
// may be unrelated directories inside OutputDir.
// Returns an error if outputDir cannot be read, or if any *.json file found
// is not a valid gobbler schema.
func BuildFileCatalog(outputDir string) (catalog.Catalog, error) {
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		return nil, fmt.Errorf("BuildFileCatalog: read %q: %w", outputDir, err)
	}
	cat := make(catalog.Catalog)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		subdir := entry.Name()
		subdirPath := filepath.Join(outputDir, subdir)

		matches, err := filepath.Glob(filepath.Join(subdirPath, "*.json"))
		if err != nil || len(matches) == 0 {
			continue // no schema file — not a type directory
		}

		data, err := os.ReadFile(matches[0])
		if err != nil {
			return nil, fmt.Errorf("BuildFileCatalog: read schema in %q: %w", subdir, err)
		}

		var sf schemaFile
		if err := json.Unmarshal(data, &sf); err != nil {
			return nil, fmt.Errorf("BuildFileCatalog: parse schema in %q: %w", subdir, err)
		}
		if !validateSchema(sf) {
			return nil, fmt.Errorf("BuildFileCatalog: %q contains invalid schema (name=%q, columns=%d)",
				subdir, sf.Name, len(sf.OrderedColumns))
		}

		cat[sf.Name] = &catalog.TableEntry{
			TypeName:      sf.Name,
			StorageBucket: subdir,
			Mode:          catalog.StorageModeFile,
			OutputDir:     outputDir,
		}
	}
	return cat, nil
}

// BuildBlobCatalog lists all containers in the given Azure Blob Storage account
// and builds a catalog.Catalog from containers that hold a valid gobbler
// {typeName}.json schema blob.
//
// A container is included if its *.json blob passes all validation checks
// (valid JSON, non-empty name, non-empty orderedColumns, all column types
// recognised). Containers with no *.json blob, or whose *.json blobs all fail
// validation, are silently skipped — they are not gobbler containers.
// If a *.json blob is found but cannot be downloaded or is malformed JSON,
// a warning is logged and the container is skipped (rather than returning an
// error) so one bad container does not block access to all others.
func BuildBlobCatalog(accountName, accountKey string) (catalog.Catalog, error) {
	cred, err := azblob.NewSharedKeyCredential(accountName, accountKey)
	if err != nil {
		return nil, fmt.Errorf("BuildBlobCatalog: invalid credentials: %w", err)
	}
	serviceURL := fmt.Sprintf("https://%s.blob.core.windows.net/", accountName)
	svc, err := azblob.NewClientWithSharedKeyCredential(serviceURL, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("BuildBlobCatalog: create service client: %w", err)
	}

	ctx := context.Background()
	cat := make(catalog.Catalog)

	pager := svc.NewListContainersPager(nil)
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("BuildBlobCatalog: list containers: %w", err)
		}
		for _, item := range page.ContainerItems {
			containerName := *item.Name
			containerURL := fmt.Sprintf("%s%s", serviceURL, containerName)
			cc, err := container.NewClientWithSharedKeyCredential(containerURL, cred, nil)
			if err != nil {
				log.Printf("BuildBlobCatalog: skip container %q: create client: %v", containerName, err)
				continue
			}

			// Find *.json blobs in the container.
			blobPager := cc.NewListBlobsFlatPager(nil)
			for blobPager.More() {
				blobPage, err := blobPager.NextPage(ctx)
				if err != nil {
					log.Printf("BuildBlobCatalog: skip container %q: list blobs: %v", containerName, err)
					break
				}
				for _, blob := range blobPage.Segment.BlobItems {
					name := *blob.Name
					if !strings.HasSuffix(name, ".json") {
						continue
					}
					// Download and validate.
					dlResp, err := cc.NewBlobClient(name).DownloadStream(ctx, nil)
					if err != nil {
						log.Printf("BuildBlobCatalog: skip blob %q in %q: download: %v", name, containerName, err)
						continue
					}
					data, err := io.ReadAll(dlResp.Body)
					dlResp.Body.Close()
					if err != nil {
						log.Printf("BuildBlobCatalog: skip blob %q in %q: read body: %v", name, containerName, err)
						continue
					}
					var sf schemaFile
					if err := json.Unmarshal(data, &sf); err != nil {
						log.Printf("BuildBlobCatalog: skip blob %q in %q: invalid JSON: %v", name, containerName, err)
						continue
					}
					if !validateSchema(sf) {
						continue // not a gobbler schema — silently skip
					}
					cat[sf.Name] = &catalog.TableEntry{
						TypeName:      sf.Name,
						StorageBucket: containerName,
						Mode:          catalog.StorageModeBlob,
						AccountName:   accountName,
						AccountKey:    accountKey,
					}
				}
			}
		}
	}
	return cat, nil
}
