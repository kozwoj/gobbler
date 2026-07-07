package query

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blockblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"
	"github.com/kozwoj/gobbler-query/query/catalog"
)

// ── blob test helpers ─────────────────────────────────────────────────────────

type catalogBlobSecrets struct {
	AccountName string `json:"accountName"`
	AccountKey  string `json:"accountKey"`
}

func loadCatalogBlobSecrets(t *testing.T) catalogBlobSecrets {
	t.Helper()
	data, err := os.ReadFile("../tester/secrets.json")
	if err != nil {
		t.Skip("../tester/secrets.json not found — skipping blob integration test")
	}
	var s catalogBlobSecrets
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("parse secrets.json: %v", err)
	}
	return s
}

func uniqueContainer(base string) string {
	return fmt.Sprintf("g-%s-%x", base, time.Now().UnixNano())
}

// writeSchema writes a minimal valid {typeName}.json into dir.
func writeSchema(t *testing.T, dir, typeName string) {
	t.Helper()
	type col struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	type schema struct {
		Name           string `json:"name"`
		OrderedColumns []col  `json:"orderedColumns"`
	}
	data, err := json.Marshal(schema{
		Name: typeName,
		OrderedColumns: []col{
			{Name: "ingest_time", Type: "datetime"},
			{Name: "value", Type: "string"},
		},
	})
	if err != nil {
		t.Fatalf("writeSchema marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, typeName+".json"), data, 0600); err != nil {
		t.Fatalf("writeSchema write: %v", err)
	}
}

// makeTypeDir creates a subdir under outputDir and writes a schema file into it.
// Returns the subdir name.
func makeTypeDir(t *testing.T, outputDir, subdirName, typeName string) string {
	t.Helper()
	subdir := filepath.Join(outputDir, subdirName)
	if err := os.MkdirAll(subdir, 0700); err != nil {
		t.Fatalf("makeTypeDir mkdir: %v", err)
	}
	writeSchema(t, subdir, typeName)
	return subdirName
}

// ── happy path ────────────────────────────────────────────────────────────────

func TestBuildFileCatalog_TwoTypes(t *testing.T) {
	outputDir := t.TempDir()
	makeTypeDir(t, outputDir, "alpha-folder", "alpha")
	makeTypeDir(t, outputDir, "beta", "beta")

	cat, err := BuildFileCatalog(outputDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cat) != 2 {
		t.Fatalf("len(cat) = %d, want 2", len(cat))
	}

	alpha := cat["alpha"]
	if alpha == nil {
		t.Fatal("cat[\"alpha\"] is nil")
	}
	if alpha.TypeName != "alpha" {
		t.Errorf("TypeName = %q, want %q", alpha.TypeName, "alpha")
	}
	if alpha.StorageBucket != "alpha-folder" {
		t.Errorf("StorageBucket = %q, want %q", alpha.StorageBucket, "alpha-folder")
	}
	if alpha.Mode != catalog.StorageModeFile {
		t.Errorf("Mode = %d, want StorageModeFile", alpha.Mode)
	}
	if alpha.OutputDir != outputDir {
		t.Errorf("OutputDir = %q, want %q", alpha.OutputDir, outputDir)
	}
}

func TestBuildFileCatalog_EmptyOutputDir(t *testing.T) {
	cat, err := BuildFileCatalog(t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cat) != 0 {
		t.Errorf("len(cat) = %d, want 0", len(cat))
	}
}

func TestBuildFileCatalog_SubdirWithNoJSON_Skipped(t *testing.T) {
	outputDir := t.TempDir()
	// subdir with no json file
	if err := os.MkdirAll(filepath.Join(outputDir, "logs"), 0700); err != nil {
		t.Fatal(err)
	}
	// subdir with a json file
	makeTypeDir(t, outputDir, "alpha", "alpha")

	cat, err := BuildFileCatalog(outputDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cat) != 1 {
		t.Fatalf("len(cat) = %d, want 1", len(cat))
	}
	if cat["alpha"] == nil {
		t.Error("expected alpha in catalog")
	}
}

// ── error cases ───────────────────────────────────────────────────────────────

func TestBuildFileCatalog_OutputDirNotExist(t *testing.T) {
	_, err := BuildFileCatalog("/no/such/directory")
	if err == nil {
		t.Fatal("expected error for non-existent outputDir, got nil")
	}
}

func TestBuildFileCatalog_InvalidJSON(t *testing.T) {
	outputDir := t.TempDir()
	subdir := filepath.Join(outputDir, "broken")
	if err := os.MkdirAll(subdir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subdir, "broken.json"), []byte("{not valid json"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := BuildFileCatalog(outputDir)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestBuildFileCatalog_EmptyName(t *testing.T) {
	outputDir := t.TempDir()
	subdir := filepath.Join(outputDir, "mytype")
	if err := os.MkdirAll(subdir, 0700); err != nil {
		t.Fatal(err)
	}
	// JSON with empty name
	data := []byte(`{"name":"","orderedColumns":[{"name":"ingest_time","type":"datetime"}]}`)
	if err := os.WriteFile(filepath.Join(subdir, "mytype.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	_, err := BuildFileCatalog(outputDir)
	if err == nil {
		t.Fatal("expected error for empty name, got nil")
	}
}

func TestBuildFileCatalog_NoColumns(t *testing.T) {
	outputDir := t.TempDir()
	subdir := filepath.Join(outputDir, "mytype")
	if err := os.MkdirAll(subdir, 0700); err != nil {
		t.Fatal(err)
	}
	data := []byte(`{"name":"mytype","orderedColumns":[]}`)
	if err := os.WriteFile(filepath.Join(subdir, "mytype.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	_, err := BuildFileCatalog(outputDir)
	if err == nil {
		t.Fatal("expected error for schema with no columns, got nil")
	}
}

// ── BuildBlobCatalog integration test ────────────────────────────────────────

// TestBuildBlobCatalog_HappyPath creates two containers — one with a valid
// gobbler schema blob and one with a non-gobbler JSON blob — and verifies that
// BuildBlobCatalog returns exactly one entry (the gobbler container) and
// silently skips the other.
func TestBuildBlobCatalog_HappyPath(t *testing.T) {
	sec := loadCatalogBlobSecrets(t)

	cred, err := azblob.NewSharedKeyCredential(sec.AccountName, sec.AccountKey)
	if err != nil {
		t.Fatalf("credentials: %v", err)
	}
	serviceURL := fmt.Sprintf("https://%s.blob.core.windows.net/", sec.AccountName)
	svc, err := azblob.NewClientWithSharedKeyCredential(serviceURL, cred, nil)
	if err != nil {
		t.Fatalf("service client: %v", err)
	}
	ctx := context.Background()

	// Container 1: valid gobbler schema blob.
	gobblerContainer := uniqueContainer("cat-g")
	// Container 2: non-gobbler JSON blob (should be skipped).
	otherContainer := uniqueContainer("cat-o")

	t.Cleanup(func() {
		svc.DeleteContainer(ctx, gobblerContainer, nil) //nolint
		svc.DeleteContainer(ctx, otherContainer, nil)   //nolint
	})

	// Create containers.
	for _, name := range []string{gobblerContainer, otherContainer} {
		if _, err := svc.CreateContainer(ctx, name, nil); err != nil {
			t.Fatalf("create container %q: %v", name, err)
		}
	}

	// Upload valid gobbler schema to gobblerContainer.
	gobblerSchema := []byte(`{"name":"cattest","orderedColumns":[{"name":"ingest_time","type":"datetime"},{"name":"val","type":"string"}]}`)
	cc1, _ := container.NewClientWithSharedKeyCredential(serviceURL+gobblerContainer, cred, nil)
	if _, err := cc1.NewBlockBlobClient("cattest.json").UploadStream(ctx, bytes.NewReader(gobblerSchema), (*blockblob.UploadStreamOptions)(nil)); err != nil {
		t.Fatalf("upload schema: %v", err)
	}

	// Upload non-gobbler JSON to otherContainer.
	otherJSON := []byte(`{"message":"hello","items":[1,2,3]}`)
	cc2, _ := container.NewClientWithSharedKeyCredential(serviceURL+otherContainer, cred, nil)
	if _, err := cc2.NewBlockBlobClient("data.json").UploadStream(ctx, bytes.NewReader(otherJSON), (*blockblob.UploadStreamOptions)(nil)); err != nil {
		t.Fatalf("upload other json: %v", err)
	}

	cat, err := BuildBlobCatalog(sec.AccountName, sec.AccountKey)
	if err != nil {
		t.Fatalf("BuildBlobCatalog: %v", err)
	}

	entry := cat["cattest"]
	if entry == nil {
		t.Fatal("catalog missing entry for \"cattest\"")
	}
	if entry.TypeName != "cattest" {
		t.Errorf("TypeName = %q, want %q", entry.TypeName, "cattest")
	}
	if entry.StorageBucket != gobblerContainer {
		t.Errorf("StorageBucket = %q, want %q", entry.StorageBucket, gobblerContainer)
	}
	if entry.Mode != catalog.StorageModeBlob {
		t.Errorf("Mode = %d, want StorageModeBlob", entry.Mode)
	}
	if entry.AccountName != sec.AccountName {
		t.Errorf("AccountName = %q, want %q", entry.AccountName, sec.AccountName)
	}
	// otherContainer must not appear in the catalog.
	for _, e := range cat {
		if e.StorageBucket == otherContainer {
			t.Errorf("non-gobbler container %q should have been skipped", otherContainer)
		}
	}
}
