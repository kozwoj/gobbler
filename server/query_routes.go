package server

import (
	"encoding/json"
	"log"
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
	gqlapi "github.com/kozwoj/gobbler-query/api"
	gqlcatalog "github.com/kozwoj/gobbler-query/query/catalog"
	"github.com/kozwoj/gobbler/items"
	"github.com/kozwoj/gobbler/pipeline"
	gobblerquery "github.com/kozwoj/gobbler/query"
)

func (s *Server) queryRoutes(r chi.Router) {
	r.Get("/", s.handleQueryDiscovery)

	r.Post("/", s.handleQuery)
	r.Get("/help/", s.handleQueryHelp)

	r.Get("/tables", s.handleQueryTables)
	r.Get("/tables/", s.handleQueryTablesHelp)
}

func (s *Server) handleQueryDiscovery(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, map[string]interface{}{
		"description": "GQL query execution and catalog discovery",
		"available_routes": []string{
			"/gobbler/query/tables",
			"/gobbler/query",
		},
		"help": "Add trailing slash to a command path for details",
	})
}

func (s *Server) handleQueryTablesHelp(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, map[string]interface{}{
		"description": "Returns the list of queryable types discovered in storage. Requires pipeline/configure. The pipeline does not need to be running.",
		"method":      "GET",
		"path":        "/gobbler/query/tables",
		"output":      `[{"typeName": "alpha", "storageBucket": "alpha-folder", "mode": "file"}, ...]`,
	})
}

func (s *Server) handleQueryHelp(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, map[string]interface{}{
		"description": "Executes a GQL query against stored data. Requires pipeline/configure. The pipeline does not need to be running.",
		"method":      "POST",
		"path":        "/gobbler/query",
		"input":       `{"query": "<gql query string>"}`,
		"output":      `[{"col": val, ...}, ...]`,
	})
}

// handleQueryTables returns the list of queryable types from s.catalog.
// Requires pipeline/configure; the pipeline does not need to be running.
func (s *Server) handleQueryTables(w http.ResponseWriter, r *http.Request) {
	type tableEntry struct {
		TypeName      string `json:"typeName"`
		StorageBucket string `json:"storageBucket"`
		Mode          string `json:"mode"`
	}

	s.mu.RLock()
	cfg := s.config
	entries := make([]tableEntry, 0, len(s.catalog))
	for _, e := range s.catalog {
		modeStr := "file"
		if e.Mode == gqlcatalog.StorageModeBlob {
			modeStr = "blob"
		}
		entries = append(entries, tableEntry{
			TypeName:      e.TypeName,
			StorageBucket: e.StorageBucket,
			Mode:          modeStr,
		})
	}
	s.mu.RUnlock()

	if cfg == nil {
		sendError(w, http.StatusConflict, "pipeline not configured; call pipeline/configure first")
		return
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].TypeName < entries[j].TypeName })
	sendJSON(w, entries)
}

// buildCatalog constructs a catalog.Catalog from storage using the current config.
func (s *Server) buildCatalog(cfg *pipeline.Config) (gqlcatalog.Catalog, error) {
	switch cfg.Mode {
	case pipeline.StorageModeFile:
		return gobblerquery.BuildFileCatalog(cfg.OutputDir)
	case pipeline.StorageModeBlob:
		return gobblerquery.BuildBlobCatalog(cfg.AccountName, cfg.AccountKey)
	default:
		return make(gqlcatalog.Catalog), nil
	}
}

// rebuildCatalog rebuilds s.catalog by scanning storage.
// Must be called with s.mu write-locked. Errors are logged but do not
// fail the calling operation — the catalog is best-effort.
func (s *Server) rebuildCatalog() {
	if s.config == nil {
		return
	}
	cat, err := s.buildCatalog(s.config)
	if err != nil {
		log.Printf("rebuildCatalog: %v", err)
		return
	}
	s.catalog = cat
}

// addCatalogEntry adds a single type entry to s.catalog using copy-on-write,
// so that concurrent readers holding a reference to the previous map are unaffected.
// Must be called with s.mu write-locked.
func (s *Server) addCatalogEntry(def items.ItemDefinition) {
	if s.config == nil {
		return
	}
	newCat := make(gqlcatalog.Catalog, len(s.catalog)+1)
	for k, v := range s.catalog {
		newCat[k] = v
	}
	entry := &gqlcatalog.TableEntry{
		TypeName:      def.TypeName,
		StorageBucket: def.Folder,
		Mode:          gqlcatalog.StorageModeFile,
		OutputDir:     s.config.OutputDir,
	}
	if s.config.Mode == pipeline.StorageModeBlob {
		entry.Mode = gqlcatalog.StorageModeBlob
		entry.AccountName = s.config.AccountName
		entry.AccountKey = s.config.AccountKey
		entry.OutputDir = ""
	}
	newCat[def.TypeName] = entry
	s.catalog = newCat
}

// handleQuery executes a GQL query against stored data and returns the result
// as a JSON array of row objects. Requires pipeline/configure; the pipeline
// does not need to be running.
func (s *Server) handleQuery(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Query string `json:"query"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if strings.TrimSpace(req.Query) == "" {
		sendError(w, http.StatusBadRequest, "missing or empty 'query' field")
		return
	}

	s.mu.RLock()
	cfg := s.config
	cat := s.catalog // safe: copy-on-write ensures this map instance is never mutated
	s.mu.RUnlock()

	if cfg == nil {
		sendError(w, http.StatusConflict, "pipeline not configured; call pipeline/configure first")
		return
	}
	if cat == nil {
		cat = make(gqlcatalog.Catalog)
	}

	result, err := gqlapi.Execute(req.Query, cat, 0)
	if err != nil {
		if isQueryClientError(err) {
			sendError(w, http.StatusBadRequest, err.Error())
		} else {
			sendError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	data, err := gobblerquery.SerializeResult(result)
	if err != nil {
		sendError(w, http.StatusInternalServerError, "serialize result: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(data) //nolint:errcheck
}

// isQueryClientError reports whether an error from api.Execute is caused by
// an invalid query (parse, plan, or validate phase) and should be mapped to
// HTTP 400. Execution and I/O errors map to 500.
func isQueryClientError(err error) bool {
	msg := err.Error()
	return strings.HasPrefix(msg, "parse:") ||
		strings.HasPrefix(msg, "plan:") ||
		strings.HasPrefix(msg, "validate:")
}
