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
	"strconv"
	"strings"
	"testing"

	"github.com/youthesame/elport/internal/frontmatter"
	"github.com/youthesame/elport/internal/plan"
	"github.com/youthesame/elport/internal/render"
	"github.com/youthesame/elport/internal/state"
	"github.com/youthesame/elport/internal/sync"
)

type itfInt int

func (n *itfInt) UnmarshalJSON(data []byte) error {
	var wrapped struct {
		BigInt string `json:"#bigint"`
	}
	if err := json.Unmarshal(data, &wrapped); err == nil && wrapped.BigInt != "" {
		v, err := strconv.Atoi(wrapped.BigInt)
		if err != nil {
			return err
		}
		*n = itfInt(v)
		return nil
	}
	var plain int
	if err := json.Unmarshal(data, &plain); err != nil {
		return err
	}
	*n = itfInt(plain)
	return nil
}

type itfTrace struct {
	Vars   []string   `json:"vars"`
	States []itfState `json:"states"`
}

type itfState struct {
	LocalBody              string `json:"localBody"`
	LocalFormat            string `json:"localFormat"`
	HasID                  bool   `json:"hasId"`
	ListedSecret           bool   `json:"listedSecret"`
	CurrentUser            string `json:"currentUser"`
	LocalMetaRev           itfInt `json:"localMetaRev"`
	SidecarsExist          bool   `json:"sidecarsExist"`
	HasBase                bool   `json:"hasBase"`
	LocalBase              string `json:"localBase"`
	RemoteBase             string `json:"remoteBase"`
	SavedContentType       itfInt `json:"savedContentType"`
	MetaBaseRev            itfInt `json:"metaBaseRev"`
	PendingCreate          bool   `json:"pendingCreate"`
	BodySent               bool   `json:"bodySent"`
	HasPendingRemote       bool   `json:"hasPendingRemote"`
	PendingRemote          string `json:"pendingRemote"`
	RememberedSecret       bool   `json:"rememberedSecret"`
	HasPendingPublish      bool   `json:"hasPendingPublish"`
	PendingPublishTargetCT itfInt `json:"pendingPublishTargetCT"`
	PendingPublishUser     string `json:"pendingPublishUser"`
	RemoteExists           bool   `json:"remoteExists"`
	RemoteBody             string `json:"remoteBody"`
	RemoteLocked           bool   `json:"remoteLocked"`
	RemoteIsHtml           bool   `json:"remoteIsHtml"`
	RemoteOwner            string `json:"remoteOwner"`
	RemoteMetaRev          itfInt `json:"remoteMetaRev"`
	SecretUploaded         bool   `json:"secretUploaded"`
	LastAction             string `json:"lastAction"`
	LastArg                string `json:"lastArg"`
	LastOutcome            string `json:"lastOutcome"`
	PrevHasConfirm         bool   `json:"prevHasConfirm"`
	PrevConfirmBody        string `json:"prevConfirmBody"`
}

const (
	figDownloadURL   = "https://e.example/app/download.php?f=ab/100-fig.png&storage=1"
	webHTML1Concrete = "<p>web html 1</p>\n"
	webHTML2Concrete = "<p>web html 2</p>\n"
	btHTMLSecretBody = "<p>`<img src=\"secret.csv\">`</p>\n"
)

func quintUserID(name string) json.Number {
	if name == "collab" {
		return json.Number("2")
	}
	return json.Number("1")
}

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

func mustRenderHTML(md string) string {
	if md == "" {
		return ""
	}
	out, err := render.MarkdownToHTML(md)
	if err != nil {
		panic(err)
	}
	return out
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
	case "v1|html":
		return mustRenderHTML(concreteRemote("v1|norm", ""))
	case "v2|html":
		return mustRenderHTML(concreteRemote("v2|norm", ""))
	case "v3|html":
		return mustRenderHTML(concreteRemote("v3|norm", ""))
	case "link_secret|html":
		return mustRenderHTML(concreteRemote("link_secret|norm", secretURL))
	case "web_html_1":
		return webHTML1Concrete
	case "web_html_2":
		return webHTML2Concrete
	case "bt_html_secret":
		return btHTMLSecretBody
	default:
		return ""
	}
}

func abstractBody(text string) string {
	hasDownload := strings.Contains(text, "https://e.example/app/download.php?f=")
	switch {
	case text == "":
		return ""
	case text == webHTML1Concrete:
		return "web_html_1"
	case text == webHTML2Concrete:
		return "web_html_2"
	case text == btHTMLSecretBody:
		return "bt_html_secret"
	case strings.HasPrefix(text, "<p>v1 "):
		return "v1|html"
	case strings.HasPrefix(text, "<p>v2 "):
		return "v2|html"
	case strings.HasPrefix(text, "<p>v3 "):
		return "v3|html"
	case strings.HasPrefix(text, "<p><a href="):
		return "link_secret|html"
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

	secretDownloadURL := func() string {
		for _, u := range api.uploads["experiments/42"] {
			if fmt.Sprint(u["real_name"]) == "secret.csv" {
				return fmt.Sprintf("https://e.example/app/download.php?f=%s&name=secret.csv&storage=1", strings.ReplaceAll(fmt.Sprint(u["long_name"]), "/", "%2F"))
			}
		}
		return ""
	}

	writeDocFile := func(hasID bool, localFormat string, listedSecret bool, metaRev int, body string) {
		m := frontmatter.NewMeta()
		if hasID {
			m.Set("id", 42)
			m.Set("entity", "experiments")
			m.Set("profile", "test")
		}
		if localFormat != "" {
			m.Set("format", localFormat)
		}
		m.Set("title", "Quint")
		if metaRev > 0 {
			m.Set("category", fmt.Sprintf("Cat%d", metaRev))
		}
		if listedSecret {
			m.Set("attachments", []string{"secret.csv"})
		}
		if err := os.WriteFile(docPath, []byte(frontmatter.Render(m, body)), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	s0 := trace.States[0]
	api.me["userid"] = quintUserID(s0.CurrentUser)
	writeDocFile(s0.HasID, s0.LocalFormat, s0.ListedSecret, int(s0.LocalMetaRev), concreteLocal(s0.LocalBody))
	cfg := testConfig()

	hasUploadedSecret := func() bool {
		for _, name := range api.uploaded {
			if name == "secret.csv" {
				return true
			}
		}
		return false
	}

	confirmHashFor := func(sym string) string {
		if sym == "" {
			return sync.ShortHash(sync.BodySHA256(""))
		}

		if cur, ok := api.entities["experiments/42"]; ok {
			if curBody, ok := cur["body"].(string); ok && abstractBody(curBody) == sym {
				return sync.ShortHash(sync.BodySHA256(curBody))
			}
		}
		return sync.ShortHash(sync.BodySHA256(concreteRemote(sym, secretDownloadURL())))
	}

	for idx := 1; idx < len(trace.States); idx++ {
		prev := trace.States[idx-1]
		want := trace.States[idx]
		var out, errBuf bytes.Buffer
		var gotOutcome string

		emptyBodyForce := prev.LocalBody == "" && (prev.LocalFormat == "html" || prev.SavedContentType == 1 || prev.RemoteIsHtml || want.LastOutcome == "pushed")

		switch want.LastAction {
		case "local_edit":
			doc, err := sync.ReadDocument(docPath, cfg)
			if err != nil {
				t.Fatalf("step %d: read doc: %v", idx, err)
			}
			writeDocFile(doc.HasID, prev.LocalFormat, prev.ListedSecret, int(prev.LocalMetaRev), concreteLocal(want.LastArg))
			gotOutcome = "ok"

		case "set_format":
			doc, err := sync.ReadDocument(docPath, cfg)
			if err != nil {
				t.Fatalf("step %d: read doc: %v", idx, err)
			}
			writeDocFile(doc.HasID, want.LocalFormat, prev.ListedSecret, int(prev.LocalMetaRev), string(doc.Body))
			gotOutcome = "ok"

		case "list_secret", "unlist_secret":
			doc, err := sync.ReadDocument(docPath, cfg)
			if err != nil {
				t.Fatalf("step %d: read doc: %v", idx, err)
			}
			writeDocFile(doc.HasID, prev.LocalFormat, want.ListedSecret, int(prev.LocalMetaRev), string(doc.Body))
			gotOutcome = "ok"

		case "legacy_stage1_state":
			saved, err := state.Load("https://e.example", "experiments", "42")
			if err != nil || saved == nil {
				t.Fatalf("step %d: load state for legacy_stage1_state: %v", idx, err)
			}
			delete(saved, "content_type")
			if err := state.Save("https://e.example", "experiments", "42", saved); err != nil {
				t.Fatalf("step %d: save legacy state: %v", idx, err)
			}
			gotOutcome = "ok"

		case "switch_machine":
			setIsolatedHome(t)
			_ = os.Remove(sync.SidecarPath(docPath))
			_ = os.Remove(sync.BaseSidecarPath(docPath))
			api.me["userid"] = quintUserID(want.LastArg)
			gotOutcome = "ok"

		case "web_edit":
			doc := api.entities["experiments/42"]
			doc["body"] = concreteRemote(want.LastArg, "")
			gotOutcome = "ok"

		case "web_edit_meta":
			doc := api.entities["experiments/42"]
			doc["category_title"] = fmt.Sprintf("Cat%d", want.RemoteMetaRev)
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

		case "web_set_owner":
			api.entities["experiments/42"]["userid"] = quintUserID(want.LastArg)
			gotOutcome = "ok"

		case "push_create_fail_before_patch":
			api.failUploadOnce = true
			_, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{Force: emptyBodyForce, WorkDir: dir}, &out, &errBuf)
			if err == nil {
				t.Fatalf("step %d: expected interrupted push failure", idx)
			}
			gotOutcome = "interrupted"

		case "push_create_lost_response":
			ensureFigUpload("experiments/42")
			api.failVerifyOnce = true
			_, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{Force: emptyBodyForce, WorkDir: dir}, &out, &errBuf)
			if err == nil {
				t.Fatalf("step %d: expected lost-response push failure", idx)
			}
			gotOutcome = "interrupted"

		case "push_publish_fail_before_patch", "push_publish_lost_response":
			ensureFigUpload("experiments/42")
			opts := sync.PushOptions{Force: emptyBodyForce, WorkDir: dir}
			if want.PrevHasConfirm {
				opts.ConfirmRemote = confirmHashFor(want.PrevConfirmBody)
			}
			if want.LastAction == "push_publish_fail_before_patch" {
				api.failPatchOnce = true
				if !prev.RemoteExists && want.RemoteIsHtml {
					api.createContentType = 1
				}
			} else {
				api.failVerifyOnce = true
			}
			_, err := sync.Push(context.Background(), docPath, api, cfg, opts, &out, &errBuf)
			api.createContentType = 0
			if err == nil {
				t.Fatalf("step %d (%s): expected interrupted publish failure", idx, want.LastAction)
			}
			gotOutcome = "interrupted"

		case "push", "push_force", "push_confirm":
			ensureFigUpload("experiments/42")
			opts := sync.PushOptions{
				Force:   want.LastAction == "push_force" || emptyBodyForce,
				WorkDir: dir,
			}
			if want.LastAction == "push_confirm" {
				opts.ConfirmRemote = confirmHashFor(want.LastArg)
			}
			_, err := sync.Push(context.Background(), docPath, api, cfg, opts, &out, &errBuf)
			gotOutcome = classifySyncOutcome("push", err, prev.LocalFormat == "html" || prev.SavedContentType == 1 || prev.RemoteIsHtml, "")

		case "push_mid_retry_conflict":
			ensureFigUpload("experiments/42")
			curDoc := copyEntityMap(api.entities["experiments/42"])
			nextDoc := copyEntityMap(curDoc)
			nextDoc["body"] = concreteRemote(want.LastArg, "")
			api.entities["experiments/42"] = nextDoc
			api.getQueue = []map[string]any{curDoc, nextDoc}
			_, err := sync.Push(context.Background(), docPath, api, cfg, sync.PushOptions{WorkDir: dir}, &out, &errBuf)
			gotOutcome = classifySyncOutcome("push", err, false, "")

		case "push_pre_patch_reget_blocked":
			ensureFigUpload("experiments/42")
			curDoc := copyEntityMap(api.entities["experiments/42"])
			curDoc["title"] = "PrePatchTrigger"
			nextDoc := copyEntityMap(curDoc)
			nextDoc["body"] = concreteRemote(want.RemoteBody, "")
			if want.RemoteIsHtml {
				nextDoc["content_type"] = 1
			} else {
				nextDoc["content_type"] = 2
			}
			if want.RemoteLocked {
				nextDoc["locked"] = 1
			} else {
				nextDoc["locked"] = 0
			}
			nextDoc["userid"] = quintUserID(want.RemoteOwner)
			api.entities["experiments/42"] = nextDoc
			api.getQueue = []map[string]any{curDoc, nextDoc}

			var origSaved map[string]any
			if prev.HasBase && prev.LocalBody == prev.LocalBase && prev.RemoteBody == prev.RemoteBase {
				if loaded, lerr := state.Load("https://e.example", "experiments", "42"); lerr == nil && loaded != nil {
					origSaved = copyEntityMap(loaded)
					perturbed := copyEntityMap(loaded)
					perturbed["meta_base"] = map[string]any{"title": "PrePatchOld"}
					_ = state.Save("https://e.example", "experiments", "42", perturbed)
				}
			}
			opts := sync.PushOptions{Force: emptyBodyForce, WorkDir: dir}
			if want.PrevHasConfirm {
				opts.ConfirmRemote = confirmHashFor(want.PrevConfirmBody)
			}
			_, err := sync.Push(context.Background(), docPath, api, cfg, opts, &out, &errBuf)
			if origSaved != nil {
				_ = state.Save("https://e.example", "experiments", "42", origSaved)
			}
			gotOutcome = classifySyncOutcome("push", err, true, "")

		case "push_post_reget_race":
			ensureFigUpload("experiments/42")
			api.patchTransform = func(doc map[string]any) map[string]any {
				if want.RemoteLocked {
					doc["locked"] = 1
				} else {
					doc["locked"] = 0
				}
				return doc
			}
			opts := sync.PushOptions{Force: emptyBodyForce, WorkDir: dir}
			if want.PrevHasConfirm {
				opts.ConfirmRemote = confirmHashFor(want.PrevConfirmBody)
			}
			_, err := sync.Push(context.Background(), docPath, api, cfg, opts, &out, &errBuf)
			api.patchTransform = nil
			if err != nil {
				t.Fatalf("step %d (push_post_reget_race): unexpected error: %v", idx, err)
			}
			gotOutcome = "pushed_raced"

		case "pull":
			_, err := sync.Pull(context.Background(), docPath, api, cfg, "", &out, &errBuf)
			if err != nil {
				gotOutcome = classifySyncOutcome("pull", err, prev.SavedContentType == 1 || prev.RemoteIsHtml, "")
			} else if strings.Contains(out.String(), "html; body untouched") {
				gotOutcome = "pulled_meta"
			} else if strings.Contains(out.String(), "remote: unchanged") {
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
				t.Fatalf("step %d (%s): remoteBody = %q (raw %q), want %q", idx, want.LastAction, gotRemote, api.entities["experiments/42"]["body"], want.RemoteBody)
			}
			gotRemoteIsHTML := sync.IsHTML(api.entities["experiments/42"])
			if gotRemoteIsHTML != want.RemoteIsHtml {
				t.Fatalf("step %d (%s): remoteIsHtml = %v, want %v", idx, want.LastAction, gotRemoteIsHTML, want.RemoteIsHtml)
			}
		}
		saved, err := state.Load("https://e.example", "experiments", "42")
		if err != nil {
			t.Fatalf("step %d: state.Load: %v", idx, err)
		}
		gotHasBase := sync.HasBase(saved)
		if gotHasBase != want.HasBase {
			t.Fatalf("step %d (%s): hasBase = %v, want %v (saved=%#v)", idx, want.LastAction, gotHasBase, want.HasBase, saved)
		}
		if gotHasBase {
			if gotLB := abstractBody(fmt.Sprint(saved["local_base"])); gotLB != want.LocalBase {
				t.Fatalf("step %d (%s): localBase = %q, want %q", idx, want.LastAction, gotLB, want.LocalBase)
			}
			if gotRB := abstractBody(fmt.Sprint(saved["remote_base"])); gotRB != want.RemoteBase {
				t.Fatalf("step %d (%s): remoteBase = %q, want %q", idx, want.LastAction, gotRB, want.RemoteBase)
			}
			if gotCT := sync.SavedContentType(saved); gotCT != int(want.SavedContentType) {
				t.Fatalf("step %d (%s): savedContentType = %d, want %d", idx, want.LastAction, gotCT, want.SavedContentType)
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
		pp, gotHPP := sync.PendingPublish(saved)
		if gotHPP != want.HasPendingPublish {
			t.Fatalf("step %d (%s): hasPendingPublish = %v, want %v (saved=%#v)", idx, want.LastAction, gotHPP, want.HasPendingPublish, saved)
		}
		if gotHPP && pp.TargetCT != int(want.PendingPublishTargetCT) {
			t.Fatalf("step %d (%s): pendingPublishTargetCT = %d, want %d", idx, want.LastAction, pp.TargetCT, want.PendingPublishTargetCT)
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
		if want.LocalMetaRev > 0 && want.LastAction == "pull" && want.LastOutcome == "pulled_meta" {
			wantCat := fmt.Sprintf("Cat%d", want.LocalMetaRev)
			if doc.Meta.Values["category"] != wantCat {
				t.Fatalf("step %d (%s): category = %#v, want %q", idx, want.LastAction, doc.Meta.Values["category"], wantCat)
			}
		}

		if want.RememberedSecret {
			if saved == nil {
				t.Fatalf("step %d: expected saved state with remote_paths", idx)
			}
			rawPaths, _ := saved["remote_paths"].([]any)
			var persistedPaths []string
			foundSecret := false
			for _, item := range rawPaths {
				if s, ok := item.(string); ok {
					persistedPaths = append(persistedPaths, s)
					if s == "secret.csv" {
						foundSecret = true
					}
				}
			}
			if !foundSecret {
				t.Fatalf("step %d (%s): persisted remote_paths %v missing secret.csv", idx, want.LastAction, persistedPaths)
			}
			if !want.ListedSecret {
				res, err := plan.Approve(concreteLocal("link_secret"), dir, nil, nil, nil, persistedPaths, nil)
				if err != nil {
					t.Fatalf("step %d: plan.Approve: %v", idx, err)
				}
				for _, ap := range res.Files {
					if filepath.Base(ap.Path()) == "secret.csv" {
						t.Fatalf("step %d: secret.csv was approved despite being in persisted remote_paths", idx)
					}
				}
			}
		}
	}
}

func classifySyncOutcome(op string, err error, htmlContext bool, _ string) string {
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
	case strings.Contains(msg, "an earlier publish"):
		return "refused_pending_publish"
	case strings.Contains(msg, "now in markdown mode") || strings.Contains(msg, "remote mode changed"):
		return "refused_mode_mismatch"
	case strings.Contains(msg, "base unavailable; remote written"):
		return "no_base_conflict"
	case strings.Contains(msg, "base unavailable"):
		return "refused_no_base"
	case strings.Contains(msg, "refusing to overwrite existing file at sidecar path"):
		return "refused_sidecar"
	case strings.Contains(msg, "remote HTML changed"):
		return "conflict_html"
	case strings.Contains(msg, "remote changed") || strings.Contains(msg, "local changes conflict"):
		if htmlContext && op == "push" {
			return "conflict_html"
		}
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
