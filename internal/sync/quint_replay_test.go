package sync_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/youthesame/elport/internal/frontmatter"
	"github.com/youthesame/elport/internal/plan"
	"github.com/youthesame/elport/internal/state"
	"github.com/youthesame/elport/internal/sync"
)

type itfTrace struct {
	Vars   []string   `json:"vars"`
	States []itfState `json:"states"`
}

type itfState struct {
	LocalBody        string `json:"localBody"`
	HasID            bool   `json:"hasId"`
	ListedSecret     bool   `json:"listedSecret"`
	SidecarsExist    bool   `json:"sidecarsExist"`
	HasBase          bool   `json:"hasBase"`
	LocalBase        string `json:"localBase"`
	RemoteBase       string `json:"remoteBase"`
	PendingCreate    bool   `json:"pendingCreate"`
	BodySent         bool   `json:"bodySent"`
	HasPendingRemote bool   `json:"hasPendingRemote"`
	PendingRemote    string `json:"pendingRemote"`
	RememberedSecret bool   `json:"rememberedSecret"`
	RemoteExists     bool   `json:"remoteExists"`
	RemoteBody       string `json:"remoteBody"`
	RemoteLocked     bool   `json:"remoteLocked"`
	RemoteIsHtml     bool   `json:"remoteIsHtml"`
	SecretUploaded   bool   `json:"secretUploaded"`
	LastAction       string `json:"lastAction"`
	LastArg          string `json:"lastArg"`
	LastOutcome      string `json:"lastOutcome"`
}

const figDownloadURL = "https://e.example/app/download.php?f=ab/100-fig.png&storage=1"

func concreteLocal(sym string) string {
	switch sym {
	case "v1":
		return "v1 ![a](fig.png)\n"
	case "v2":
		return "v2 ![a](fig.png)\n"
	case "v3":
		return "v3 ![a](fig.png)\n"
	case "link_secret":
		return "[s](secret.csv) ![a](fig.png)\n"
	default:
		return ""
	}
}

func concreteRemote(sym string, secretURL string) string {
	switch sym {
	case "v1|norm":
		return "v1 ![a](" + figDownloadURL + ")\n"
	case "v2|norm":
		return "v2 ![a](" + figDownloadURL + ")\n"
	case "v3|norm":
		return "v3 ![a](" + figDownloadURL + ")\n"
	case "link_secret|norm":
		if secretURL != "" {
			return "[s](" + secretURL + ") ![a](" + figDownloadURL + ")\n"
		}
		return "[s](secret.csv) ![a](" + figDownloadURL + ")\n"
	default:
		return ""
	}
}

func abstractBody(text string) string {
	hasDownload := strings.Contains(text, "https://e.example/app/download.php?f=")
	switch {
	case text == "":
		return ""
	case strings.HasPrefix(text, "v1 "):
		if hasDownload {
			return "v1|norm"
		}
		return "v1"
	case strings.HasPrefix(text, "v2 "):
		if hasDownload {
			return "v2|norm"
		}
		return "v2"
	case strings.HasPrefix(text, "v3 "):
		if hasDownload {
			return "v3|norm"
		}
		return "v3"
	case strings.HasPrefix(text, "[s]("):
		if hasDownload {
			return "link_secret|norm"
		}
		return "link_secret"
	default:
		return text
	}
}

func TestQuintITFTracesReplayAgainstGoSync(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "testdata", "quint", "*.itf.json"))
	if err != nil {
		t.Fatalf("glob ITF traces: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no Quint ITF trace files found in testdata/quint/")
	}

	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			raw, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("read trace: %v", err)
			}
			var trace itfTrace
			if err := json.Unmarshal(raw, &trace); err != nil {
				t.Fatalf("parse ITF JSON: %v", err)
			}
			if len(trace.States) < 2 {
				t.Fatalf("trace too short: %d states", len(trace.States))
			}
			replayTrace(t, trace)
		})
	}
}

func replayTrace(t *testing.T, trace itfTrace) {
	t.Helper()
	setIsolatedHome(t)
	dir := t.TempDir()
	docPath := filepath.Join(dir, "report.md")
	figBytes := []byte("fig-png-bytes")
	secretBytes := []byte("secret-csv-bytes")
	if err := os.WriteFile(filepath.Join(dir, "fig.png"), figBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "secret.csv"), secretBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	figSum := sha256.Sum256(figBytes)

	api := newWriteFakeAPI()
	// Pre-register fig.png on the server so it always reuses upload 100 and produces figDownloadURL.
	figUpload := map[string]any{
		"id":             json.Number("100"),
		"real_name":      "fig.png",
		"long_name":      "ab/100-fig.png",
		"storage":        1,
		"hash_algorithm": "sha256",
		"hash":           hex.EncodeToString(figSum[:]),
	}
	api.downloads[100] = figBytes
	api.nextID = 42
	api.nextUploadID = 101

	ensureFigUpload := func(entityKey string) {
		for _, u := range api.uploads[entityKey] {
			if fmt.Sprint(u["real_name"]) == "fig.png" {
				return
			}
		}
		api.uploads[entityKey] = append(api.uploads[entityKey], figUpload)
	}

	writeDocFile := func(hasID bool, listedSecret bool, body string) {
		m := frontmatter.NewMeta()
		if hasID {
			m.Set("id", 42)
			m.Set("entity", "experiments")
			m.Set("profile", "test")
		}
		m.Set("title", "Quint")
		if listedSecret {
			m.Set("attachments", []string{"secret.csv"})
		}
		if err := os.WriteFile(docPath, []byte(frontmatter.Render(m, body)), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	s0 := trace.States[0]
	writeDocFile(s0.HasID, s0.ListedSecret, concreteLocal(s0.LocalBody))
	cfg := testConfig()

	hasUploadedSecret := func() bool {
		for _, name := range api.uploaded {
			if name == "secret.csv" {
				return true
			}
		}
		return false
	}

	for idx := 1; idx < len(trace.States); idx++ {
		prev := trace.States[idx-1]
		want := trace.States[idx]
		var out, errBuf bytes.Buffer
		var gotOutcome string

		switch want.LastAction {
		case "local_edit":
			doc, err := sync.ReadDocument(docPath, cfg)
			if err != nil {
				t.Fatalf("step %d: read doc: %v", idx, err)
			}
			writeDocFile(doc.HasID, prev.ListedSecret, concreteLocal(want.LastArg))
			gotOutcome = "ok"

		case "list_secret", "unlist_secret":
			doc, err := sync.ReadDocument(docPath, cfg)
			if err != nil {
				t.Fatalf("step %d: read doc: %v", idx, err)
			}
			writeDocFile(doc.HasID, want.ListedSecret, string(doc.Body))
			gotOutcome = "ok"

		case "web_edit":
			doc := api.entities["experiments/42"]
			doc["body"] = concreteRemote(want.LastArg, "")
			gotOutcome = "ok"

		case "web_lock":
			api.entities["experiments/42"]["locked"] = 1
			gotOutcome = "ok"

		case "web_unlock":
			api.entities["experiments/42"]["locked"] = 0
			gotOutcome = "ok"

		case "web_html_on":
			api.entities["experiments/42"]["content_type"] = 1
			gotOutcome = "ok"

		case "web_html_off":
			api.entities["experiments/42"]["content_type"] = 2
			gotOutcome = "ok"

		case "push_create_fail_before_patch":
			api.failUploadOnce = true
			_, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{WorkDir: dir}, &out, &errBuf)
			if err == nil {
				t.Fatalf("step %d: expected interrupted push failure", idx)
			}
			gotOutcome = "interrupted"

		case "push_create_lost_response":
			ensureFigUpload("experiments/42")
			api.failVerifyOnce = true
			_, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{WorkDir: dir}, &out, &errBuf)
			if err == nil {
				t.Fatalf("step %d: expected lost-response push failure", idx)
			}
			gotOutcome = "interrupted"

		case "push", "push_force":
			ensureFigUpload("experiments/42")
			force := want.LastAction == "push_force"
			// If a previous conflict left stale sidecars with different content and another conflict occurs,
			// remove old sidecars only when Quint models overwriting sidecars on a new conflict with a different remoteBody.
			if prev.SidecarsExist && want.LastOutcome == "conflict" && prev.PendingRemote != want.PendingRemote {
				_ = os.Remove(sync.SidecarPath(docPath))
				_ = os.Remove(sync.BaseSidecarPath(docPath))
			}
			_, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{Force: force, WorkDir: dir}, &out, &errBuf)
			gotOutcome = classifySyncOutcome("push", err)

		case "push_mid_retry_conflict":
			ensureFigUpload("experiments/42")
			curDoc := copyEntityMap(api.entities["experiments/42"])
			nextDoc := copyEntityMap(curDoc)
			nextDoc["body"] = concreteRemote(want.LastArg, "")
			api.entities["experiments/42"] = nextDoc
			api.getQueue = []map[string]any{curDoc, nextDoc}
			_, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{WorkDir: dir}, &out, &errBuf)
			gotOutcome = classifySyncOutcome("push", err)

		case "pull":
			if prev.SidecarsExist && want.LastOutcome == "conflict" && prev.PendingRemote != want.PendingRemote {
				_ = os.Remove(sync.SidecarPath(docPath))
				_ = os.Remove(sync.BaseSidecarPath(docPath))
			}
			res, err := sync.Pull(context.Background(), docPath, api, cfg, "", &out, &errBuf)
			if err != nil {
				gotOutcome = classifySyncOutcome("pull", err)
			} else if res == sync.UpToDate {
				gotOutcome = "up_to_date"
			} else {
				gotOutcome = "pulled"
			}

		case "merge_resolved":
			if err := sync.Merge(context.Background(), docPath, cfg, "", true, &out); err != nil {
				t.Fatalf("step %d (%s): unexpected merge error: %v", idx, want.LastAction, err)
			}
			gotOutcome = "resolved"

		default:
			t.Fatalf("step %d: unknown action %q", idx, want.LastAction)
		}

		if gotOutcome != want.LastOutcome {
			t.Fatalf("step %d (%s %s): outcome = %q, want %q (stderr=%q)", idx, want.LastAction, want.LastArg, gotOutcome, want.LastOutcome, errBuf.String())
		}

		// Verify real Go implementation state matches the Quint state machine.
		doc, err := sync.ReadDocument(docPath, cfg)
		if err != nil {
			t.Fatalf("step %d: read doc after action: %v", idx, err)
		}
		if doc.HasID != want.HasID {
			t.Fatalf("step %d (%s): hasId = %v, want %v", idx, want.LastAction, doc.HasID, want.HasID)
		}
		if gotLocal := abstractBody(string(doc.Body)); gotLocal != want.LocalBody {
			t.Fatalf("step %d (%s): localBody = %q (raw %q), want %q", idx, want.LastAction, gotLocal, doc.Body, want.LocalBody)
		}
		if want.RemoteExists {
			if gotRemote := abstractBody(fmt.Sprint(api.entities["experiments/42"]["body"])); gotRemote != want.RemoteBody {
				t.Fatalf("step %d (%s): remoteBody = %q, want %q", idx, want.LastAction, gotRemote, want.RemoteBody)
			}
		}
		saved, err := state.Load("https://e.example", "experiments", "42")
		if err != nil {
			t.Fatalf("step %d: state.Load: %v", idx, err)
		}
		if (saved != nil) != want.HasBase {
			t.Fatalf("step %d (%s): hasBase = %v, want %v", idx, want.LastAction, saved != nil, want.HasBase)
		}
		if saved != nil {
			if gotLB := abstractBody(fmt.Sprint(saved["local_base"])); gotLB != want.LocalBase {
				t.Fatalf("step %d (%s): localBase = %q, want %q", idx, want.LastAction, gotLB, want.LocalBase)
			}
			if gotRB := abstractBody(fmt.Sprint(saved["remote_base"])); gotRB != want.RemoteBase {
				t.Fatalf("step %d (%s): remoteBase = %q, want %q", idx, want.LastAction, gotRB, want.RemoteBase)
			}
			gotPC, _ := saved["pending_create"].(bool)
			if gotPC != want.PendingCreate {
				t.Fatalf("step %d (%s): pendingCreate = %v, want %v", idx, want.LastAction, gotPC, want.PendingCreate)
			}
			gotBS, _ := saved["body_sent"].(bool)
			if gotBS != want.BodySent {
				t.Fatalf("step %d (%s): bodySent = %v, want %v", idx, want.LastAction, gotBS, want.BodySent)
			}
			_, gotHPR := saved["pending_remote"]
			if gotHPR != want.HasPendingRemote {
				t.Fatalf("step %d (%s): hasPendingRemote = %v, want %v", idx, want.LastAction, gotHPR, want.HasPendingRemote)
			}
		}
		_, baseErr := os.Stat(sync.BaseSidecarPath(docPath))
		_, remErr := os.Stat(sync.SidecarPath(docPath))
		gotSidecars := baseErr == nil && remErr == nil
		if gotSidecars != want.SidecarsExist {
			t.Fatalf("step %d (%s): sidecarsExist = %v, want %v", idx, want.LastAction, gotSidecars, want.SidecarsExist)
		}
		if hasUploadedSecret() != want.SecretUploaded {
			t.Fatalf("step %d (%s): secretUploaded = %v, want %v", idx, want.LastAction, hasUploadedSecret(), want.SecretUploaded)
		}

		// Also verify plan.Approve refuses secret.csv whenever it is remote-linked and not listed.
		if !want.ListedSecret && want.RememberedSecret {
			res, err := plan.Approve(concreteLocal("link_secret"), dir, nil, nil, nil, []string{"secret.csv"}, nil)
			if err != nil {
				t.Fatalf("step %d: plan.Approve: %v", idx, err)
			}
			for _, ap := range res.Files {
				if filepath.Base(ap.Path()) == "secret.csv" {
					t.Fatalf("step %d: secret.csv was approved despite being remembered remote-linked", idx)
				}
			}
		}
	}
}

func classifySyncOutcome(op string, err error) string {
	if err == nil {
		if op == "push" {
			return "pushed"
		}
		return "pulled"
	}
	var htmlErr sync.HtmlEntityError
	if errors.As(err, &htmlErr) {
		return "refused_html"
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "is locked"):
		return "refused_locked"
	case strings.Contains(msg, "base unavailable"):
		return "refused_no_base"
	case strings.Contains(msg, "remote changed") || strings.Contains(msg, "local changes conflict"):
		return "conflict"
	default:
		return "error:" + msg
	}
}

func copyEntityMap(m map[string]any) map[string]any {
	cp := make(map[string]any, len(m))
	for k, v := range m {
		cp[k] = v
	}
	return cp
}
