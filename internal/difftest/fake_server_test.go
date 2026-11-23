package difftest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

type recordedReq struct {
	Method      string         `json:"method"`
	Path        string         `json:"path"`
	Query       string         `json:"query,omitempty"`
	Body        map[string]any `json:"body,omitempty"`
	UploadName  string         `json:"upload_name,omitempty"`
	UploadBytes string         `json:"upload_bytes,omitempty"`
}

type fakeElabServer struct {
	mu           sync.Mutex
	srv          *httptest.Server
	caFile       string
	entities     map[string]map[string]any
	uploads      map[string][]map[string]any
	downloads    map[int][]byte
	comments     map[string][]map[string]any
	tags         map[string][]string
	links        map[string][][2]any
	categories   map[string][]map[string]any
	statuses     map[string][]map[string]any
	nextID       map[string]int
	nextUploadID int
	requests     []recordedReq
	normalize    bool
}

func newFakeElabServer(tmpDir string) (*fakeElabServer, error) {
	f := &fakeElabServer{
		entities:     map[string]map[string]any{},
		uploads:      map[string][]map[string]any{},
		downloads:    map[int][]byte{},
		comments:     map[string][]map[string]any{},
		tags:         map[string][]string{},
		links:        map[string][][2]any{},
		categories:   map[string][]map[string]any{},
		statuses:     map[string][]map[string]any{},
		nextID:       map[string]int{"experiments": 100, "items": 200},
		nextUploadID: 500,
		normalize:    true,
	}
	f.categories["experiments"] = []map[string]any{
		{"id": 1, "title": "Assay"},
		{"id": 2, "title": "Protocol"},
	}
	f.categories["items"] = []map[string]any{
		{"id": 3, "title": "Buffer"},
	}
	f.statuses["experiments"] = []map[string]any{
		{"id": 10, "title": "Running"},
		{"id": 11, "title": "Done"},
	}
	f.statuses["items"] = []map[string]any{
		{"id": 12, "title": "Available"},
	}

	f.srv = httptest.NewTLSServer(http.HandlerFunc(f.handle))
	cert := f.srv.Certificate()
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
	f.caFile = filepath.Join(tmpDir, "fake-ca.pem")
	if err := os.WriteFile(f.caFile, pemBytes, 0o600); err != nil {
		f.srv.Close()
		return nil, err
	}
	return f, nil
}

func (f *fakeElabServer) Close() {
	f.srv.Close()
}

func (f *fakeElabServer) URL() string {
	return f.srv.URL
}

func (f *fakeElabServer) CAFile() string {
	return f.caFile
}

func (f *fakeElabServer) ResetLog() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = nil
}

func (f *fakeElabServer) Requests() []recordedReq {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]recordedReq, len(f.requests))
	copy(out, f.requests)
	return out
}

func (f *fakeElabServer) SeedEntity(entity string, id int, doc map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := fmt.Sprintf("%s/%d", entity, id)
	cp := map[string]any{
		"id":            id,
		"title":         "Untitled",
		"body":          "",
		"content_type":  2,
		"userid":        1,
		"team":          7,
		"canread_base":  30,
		"canwrite_base": 30,
		"canread":       `{"base":30,"teams":[],"users":[],"teamgroups":[]}`,
		"canwrite":      `{"base":30,"teams":[],"users":[],"teamgroups":[]}`,
		"tags":          "",
		"locked":        0,
		"sharelink":     fmt.Sprintf("%s/%s.php?mode=view&id=%d", f.srv.URL, pageFor(entity), id),
	}
	for k, v := range doc {
		cp[k] = v
	}
	f.entities[key] = cp
}

func (f *fakeElabServer) SeedUpload(entity string, id int, realName string, data []byte) map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	uid := f.nextUploadID
	f.nextUploadID++
	sum := sha256.Sum256(data)
	entry := map[string]any{
		"id":             uid,
		"real_name":      realName,
		"long_name":      fmt.Sprintf("ab/%d-%s", uid, realName),
		"storage":        1,
		"hash_algorithm": "sha256",
		"hash":           hex.EncodeToString(sum[:]),
	}
	key := fmt.Sprintf("%s/%d", entity, id)
	f.uploads[key] = append(f.uploads[key], entry)
	f.downloads[uid] = append([]byte(nil), data...)
	return entry
}

func (f *fakeElabServer) SetRemoteBody(entity string, id int, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := fmt.Sprintf("%s/%d", entity, id)
	f.entities[key]["body"] = body
}

func (f *fakeElabServer) SetRemoteField(entity string, id int, field string, val any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := fmt.Sprintf("%s/%d", entity, id)
	f.entities[key][field] = val
}

func pageFor(entity string) string {
	if entity == "items" {
		return "database"
	}
	return "experiments"
}

func (f *fakeElabServer) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if r.Header.Get("Authorization") != "1-diff-secret-key" {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"unauthorized"}`))
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/v2")
	rec := recordedReq{
		Method: r.Method,
		Path:   path,
		Query:  r.URL.RawQuery,
	}

	var jsonBody map[string]any
	if r.Method == http.MethodPost || r.Method == http.MethodPatch {
		ct := r.Header.Get("Content-Type")
		if strings.HasPrefix(ct, "multipart/form-data") {
			if err := r.ParseMultipartForm(32 << 20); err == nil {
				file, hdr, err := r.FormFile("file")
				if err == nil {
					data, _ := io.ReadAll(file)
					_ = file.Close()
					rec.UploadName = hdr.Filename
					rec.UploadBytes = string(data)
				}
			}
		} else {
			raw, _ := io.ReadAll(r.Body)
			if len(raw) > 0 {
				_ = json.Unmarshal(raw, &jsonBody)
				rec.Body = jsonBody
			}
		}
	}
	f.requests = append(f.requests, rec)

	w.Header().Set("Content-Type", "application/json")
	parts := strings.Split(strings.Trim(path, "/"), "/")

	switch {
	case r.Method == http.MethodGet && path == "/users/me":
		_ = json.NewEncoder(w).Encode(map[string]any{
			"userid":             1,
			"fullname":           "Diff Tester",
			"email":              "diff@example.test",
			"team":               7,
			"scope_experiments":  1,
			"scope_items":        2,
			"default_read_base":  30,
			"default_write_base": 30,
			"teams": []map[string]any{
				{"id": 7, "name": "Lab", "usergroup": 4, "is_admin": 0},
			},
		})
		return
	case r.Method == http.MethodGet && path == "/info":
		_ = json.NewEncoder(w).Encode(map[string]any{"elabftw_version": "5.1.0"})
		return
	case r.Method == http.MethodGet && path == "/apikeys":
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"id": 1, "can_write": 1},
		})
		return
	case r.Method == http.MethodGet && len(parts) == 3 && parts[0] == "teams":
		switch parts[2] {
		case "experiments_categories":
			_ = json.NewEncoder(w).Encode(f.categories["experiments"])
		case "items_types":
			_ = json.NewEncoder(w).Encode(f.categories["items"])
		case "experiments_status":
			_ = json.NewEncoder(w).Encode(f.statuses["experiments"])
		case "items_status":
			_ = json.NewEncoder(w).Encode(f.statuses["items"])
		default:
			w.WriteHeader(http.StatusNotFound)
		}
		return
	}

	if len(parts) == 0 || (parts[0] != "experiments" && parts[0] != "items") {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	entity := parts[0]

	// Collection endpoints: GET /{entity}, POST /{entity}
	if len(parts) == 1 {
		if r.Method == http.MethodGet {
			offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
			limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
			if limit <= 0 {
				limit = 100
			}
			var ids []int
			for k, doc := range f.entities {
				if strings.HasPrefix(k, entity+"/") {
					if id, ok := doc["id"].(int); ok {
						ids = append(ids, id)
					}
				}
			}
			sort.Ints(ids)
			rows := make([]map[string]any, 0)
			for i := offset; i < len(ids) && len(rows) < limit; i++ {
				rows = append(rows, f.entityView(entity, ids[i]))
			}
			_ = json.NewEncoder(w).Encode(rows)
			return
		}
		if r.Method == http.MethodPost {
			id := f.nextID[entity]
			f.nextID[entity] = id + 1
			title, _ := jsonBody["title"].(string)
			key := fmt.Sprintf("%s/%d", entity, id)
			f.entities[key] = map[string]any{
				"id":            id,
				"title":         title,
				"body":          "",
				"content_type":  2,
				"userid":        1,
				"team":          7,
				"canread_base":  30,
				"canwrite_base": 30,
				"canread":       `{"base":30,"teams":[],"users":[],"teamgroups":[]}`,
				"canwrite":      `{"base":30,"teams":[],"users":[],"teamgroups":[]}`,
				"tags":          "",
				"locked":        0,
				"sharelink":     fmt.Sprintf("%s/%s.php?mode=view&id=%d", f.srv.URL, pageFor(entity), id),
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": id})
			return
		}
	}

	id, err := strconv.Atoi(parts[1])
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	key := fmt.Sprintf("%s/%d", entity, id)
	doc, exists := f.entities[key]
	if !exists {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"not found"}`))
		return
	}

	// Single entity: GET /{entity}/{id}, PATCH /{entity}/{id}
	if len(parts) == 2 {
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(f.entityView(entity, id))
			return
		}
		if r.Method == http.MethodPatch {
			for k, v := range jsonBody {
				if k == "body" {
					if s, ok := v.(string); ok {
						if f.normalize && s != "" && !strings.HasSuffix(s, "\n") {
							s += "\n"
						}
						doc["body"] = s
					}
					continue
				}
				if k == "category" {
					doc["category"] = v
					for _, c := range f.categories[entity] {
						if fmt.Sprint(c["id"]) == fmt.Sprint(v) {
							doc["category_title"] = c["title"]
						}
					}
					continue
				}
				if k == "status" {
					doc["status"] = v
					for _, s := range f.statuses[entity] {
						if fmt.Sprint(s["id"]) == fmt.Sprint(v) {
							doc["status_title"] = s["title"]
						}
					}
					continue
				}
				doc[k] = v
			}
			_ = json.NewEncoder(w).Encode(f.entityView(entity, id))
			return
		}
	}

	// Subresources: uploads, tags, comments, links
	sub := parts[2]
	switch sub {
	case "uploads":
		if len(parts) == 3 {
			if r.Method == http.MethodGet {
				list := f.uploads[key]
				if list == nil {
					list = []map[string]any{}
				}
				_ = json.NewEncoder(w).Encode(list)
				return
			}
			if r.Method == http.MethodPost {
				uid := f.nextUploadID
				f.nextUploadID++
				data := []byte(rec.UploadBytes)
				sum := sha256.Sum256(data)
				entry := map[string]any{
					"id":             uid,
					"real_name":      rec.UploadName,
					"long_name":      fmt.Sprintf("ab/%d-%s", uid, rec.UploadName),
					"storage":        1,
					"hash_algorithm": "sha256",
					"hash":           hex.EncodeToString(sum[:]),
				}
				f.uploads[key] = append(f.uploads[key], entry)
				f.downloads[uid] = data
				w.WriteHeader(http.StatusCreated)
				_ = json.NewEncoder(w).Encode(map[string]any{"id": uid})
				return
			}
		}
		if len(parts) == 4 && r.Method == http.MethodGet {
			uid, err := strconv.Atoi(parts[3])
			if err != nil {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			data, ok := f.downloads[uid]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(data)
			return
		}
	case "tags":
		if r.Method == http.MethodPost {
			tag, _ := jsonBody["tag"].(string)
			f.tags[key] = append(f.tags[key], tag)
			doc["tags"] = strings.Join(f.tags[key], "|")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": len(f.tags[key])})
			return
		}
	case "comments":
		if r.Method == http.MethodGet {
			list := f.comments[key]
			if list == nil {
				list = []map[string]any{}
			}
			_ = json.NewEncoder(w).Encode(list)
			return
		}
		if r.Method == http.MethodPost {
			text, _ := jsonBody["comment"].(string)
			f.comments[key] = append(f.comments[key], map[string]any{
				"fullname":    "Diff Tester",
				"created_at":  "2026-01-01 12:00:00",
				"modified_at": "2026-01-01 12:00:00",
				"comment":     text,
			})
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": len(f.comments[key])})
			return
		}
	case "experiments_links", "items_links":
		if len(parts) == 4 && r.Method == http.MethodPost {
			targetEntity := strings.TrimSuffix(sub, "_links")
			targetID, _ := strconv.Atoi(parts[3])
			f.links[key] = append(f.links[key], [2]any{targetEntity, targetID})
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{})
			return
		}
	}

	w.WriteHeader(http.StatusNotFound)
}

func (f *fakeElabServer) entityView(entity string, id int) map[string]any {
	key := fmt.Sprintf("%s/%d", entity, id)
	doc := f.entities[key]
	cp := make(map[string]any, len(doc)+1)
	for k, v := range doc {
		cp[k] = v
	}
	ups := f.uploads[key]
	if ups == nil {
		ups = []map[string]any{}
	}
	cp["uploads"] = ups
	return cp
}
