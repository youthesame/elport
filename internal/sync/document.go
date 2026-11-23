package sync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/idna"

	"github.com/youthesame/elport/internal/config"
	"github.com/youthesame/elport/internal/frontmatter"
	"github.com/youthesame/elport/internal/transclude"
)

const (
	LargeUploadBytes = 25 * 1024 * 1024
	UpToDate         = "up to date"
)

var EntityPages = map[string]string{
	"experiments": "experiments.php",
	"items":       "database.php",
}

var SyncedMetaKeys = []string{"title", "category", "status", "read", "write"}

type HtmlEntityError struct {
	Msg string
}

func (e HtmlEntityError) Error() string {
	return e.Msg
}

type Document struct {
	Meta   frontmatter.Meta
	Body   LocalBody
	Entity string
	ID     int
	HasID  bool
}

type Remote struct {
	API     API
	Entity  string
	ID      int
	BaseURL string
}

func (r *Remote) Get(ctx context.Context) (map[string]any, error) {
	return r.API.Get(ctx, r.Entity, r.ID)
}

func (r *Remote) Uploads(ctx context.Context, remoteDoc map[string]any) ([]transclude.Upload, error) {
	var rawList []map[string]any
	if remoteDoc != nil {
		if embedded, ok := remoteDoc["uploads"].([]any); ok {
			rawList = make([]map[string]any, 0, len(embedded))
			for _, item := range embedded {
				if m, ok := item.(map[string]any); ok {
					rawList = append(rawList, m)
				}
			}
		}
	}
	if rawList == nil {
		var err error
		rawList, err = r.API.Uploads(ctx, r.Entity, r.ID)
		if err != nil {
			return nil, err
		}
	}
	uploads := make([]transclude.Upload, 0, len(rawList))
	for _, m := range rawList {
		uploads = append(uploads, uploadFromMap(m))
	}
	return transclude.NameUploads(uploads), nil
}

func (r *Remote) Download(ctx context.Context, uid int) ([]byte, error) {
	return r.API.Download(ctx, r.Entity, r.ID, uid)
}

func uploadFromMap(m map[string]any) transclude.Upload {
	id, _ := intField(m, "id")
	storage, ok := intField(m, "storage")
	if !ok || storage == 0 {
		storage = 1
	}
	return transclude.Upload{
		ID:       id,
		RealName: strField(m, "real_name"),
		LongName: strField(m, "long_name"),
		Storage:  storage,
		Extra:    m,
	}
}

func strField(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x
	case json.Number:
		return x.String()
	default:
		return fmt.Sprint(x)
	}
}

func intField(m map[string]any, key string) (int, bool) {
	if m == nil {
		return 0, false
	}
	v, ok := m[key]
	if !ok || v == nil {
		return 0, false
	}
	switch x := v.(type) {
	case int:
		return x, true
	case int64:
		return int(x), true
	case float64:
		return int(x), true
	case json.Number:
		if n, err := x.Int64(); err == nil {
			return int(n), true
		}
	case string:
		if n, err := strconv.Atoi(x); err == nil {
			return n, true
		}
	}
	return 0, false
}

func truthyField(m map[string]any, key string) bool {
	if m == nil {
		return false
	}
	v, ok := m[key]
	if !ok || v == nil {
		return false
	}
	switch x := v.(type) {
	case bool:
		return x
	case int:
		return x != 0
	case int64:
		return x != 0
	case float64:
		return x != 0
	case json.Number:
		n, err := x.Int64()
		return err == nil && n != 0
	case string:
		return x == "1" || strings.EqualFold(x, "true")
	default:
		return false
	}
}

func ignoreLayers(cfg *config.Data) []transclude.IgnoreLayer {
	if cfg == nil || len(cfg.IgnoreLayers) == 0 {
		return nil
	}
	out := make([]transclude.IgnoreLayer, len(cfg.IgnoreLayers))
	for i, l := range cfg.IgnoreLayers {
		out[i] = transclude.IgnoreLayer{Patterns: l.Patterns, Prefix: l.Prefix}
	}
	return out
}

func ReadDocument(path string, cfg *config.Data) (*Document, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if !utf8.Valid(raw) {
		return nil, fmt.Errorf("document is not valid UTF-8: %s", path)
	}
	normalized := strings.ReplaceAll(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\r", "\n")
	meta, body, err := frontmatter.Parse(normalized)
	if err != nil {
		return nil, err
	}
	entity := "experiments"
	if cfg != nil {
		if cfgEntity, ok := cfg.Raw["entity"].(string); ok && cfgEntity != "" {
			entity = cfgEntity
		}
	}
	if e, ok := meta.Values["entity"].(string); ok && e != "" {
		entity = e
	}
	doc := &Document{
		Meta:   meta,
		Body:   LocalBody(body),
		Entity: entity,
	}
	if rawID, exists := meta.Values["id"]; exists {
		id, err := frontmatter.ValidateID(rawID)
		if err != nil {
			return nil, err
		}
		doc.ID = id
		doc.HasID = true
	}
	return doc, nil
}

func metaProfile(meta frontmatter.Meta) string {
	s, _ := meta.Values["profile"].(string)
	return s
}

func metaAttachments(meta frontmatter.Meta) []string {
	if s, ok := meta.Values["attachments"].([]string); ok {
		return s
	}
	return nil
}

func EntityURL(baseURL, entity string, eid int) string {
	return fmt.Sprintf("%s/%s?mode=view&id=%d", strings.TrimRight(baseURL, "/"), EntityPages[entity], eid)
}

func canonicalHost(rawHost string) (string, bool) {
	if rawHost == "" {
		return "", false
	}
	if addr, err := netip.ParseAddr(rawHost); err == nil {
		return addr.String(), true
	}
	ascii, err := idna.Lookup.ToASCII(rawHost)
	if err != nil || ascii == "" {
		return "", false
	}
	return strings.ToLower(ascii), true
}

func ParseEntityURL(rawURL, baseURL string) (string, int, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", 0, errors.New("invalid entity URL")
	}
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", 0, errors.New("invalid entity URL")
	}
	if !strings.EqualFold(parsed.Scheme, "https") || !strings.EqualFold(base.Scheme, "https") {
		return "", 0, errors.New("entity URL must use https://")
	}
	if parsed.User != nil {
		return "", 0, errors.New("entity URL must not include credentials")
	}
	pPort := parsed.Port()
	if pPort == "" {
		pPort = "443"
	}
	bPort := base.Port()
	if bPort == "" {
		bPort = "443"
	}
	pHost, pOK := canonicalHost(parsed.Hostname())
	bHost, bOK := canonicalHost(base.Hostname())
	if !pOK || !bOK || pHost != bHost || pPort != bPort {
		return "", 0, fmt.Errorf("URL does not match configured server %s", baseURL)
	}
	prefix := strings.TrimRight(base.Path, "/") + "/"
	if !strings.HasPrefix(parsed.Path, prefix) {
		return "", 0, fmt.Errorf("URL does not match configured server %s", baseURL)
	}
	page := parsed.Path[len(prefix):]
	entity := ""
	for e, ep := range EntityPages {
		if ep == page {
			entity = e
			break
		}
	}
	if entity == "" {
		return "", 0, errors.New("unsupported entity URL")
	}
	q, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		return "", 0, errors.New("unsupported entity URL")
	}
	if len(q) != 2 {
		return "", 0, errors.New("unsupported entity URL")
	}
	modes, hasMode := q["mode"]
	ids, hasID := q["id"]
	if !hasMode || !hasID {
		return "", 0, errors.New("unsupported entity URL")
	}
	if len(modes) != 1 || modes[0] != "view" {
		return "", 0, errors.New("unsupported entity URL (expected mode=view)")
	}
	if len(ids) != 1 || ids[0] == "" {
		return "", 0, errors.New("unsupported entity URL (expected one positive id)")
	}
	for i := range len(ids[0]) {
		if ids[0][i] < '0' || ids[0][i] > '9' {
			return "", 0, errors.New("unsupported entity URL (expected one positive id)")
		}
	}
	idNum, err := strconv.Atoi(ids[0])
	if err != nil {
		return "", 0, errors.New("unsupported entity URL (expected one positive id)")
	}
	validID, err := frontmatter.ValidateID(idNum)
	if err != nil {
		return "", 0, err
	}
	return entity, validID, nil
}

func samePath(a, b string) bool {
	ca := filepath.Clean(a)
	cb := filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(ca, cb)
	}
	return ca == cb
}

func CheckWorkingCopy(saved map[string]any, docPath, entity string, eid int) error {
	recorded := strField(saved, "path")
	if recorded == "" {
		return nil
	}
	resolved := transclude.ResolveNonStrict(docPath)
	if samePath(recorded, resolved) || samePath(transclude.ResolveNonStrict(recorded), resolved) {
		return nil
	}
	meta, ok := frontmatter.DocumentMeta(recorded)
	if !ok {
		return nil
	}
	otherID, _ := frontmatter.ValidateID(meta.Values["id"])
	otherEntity := entity
	if e, ok := meta.Values["entity"].(string); ok && e != "" {
		otherEntity = e
	}
	if otherID == eid && otherEntity == entity {
		return fmt.Errorf("%s/%d is tracked at %s; work there, or delete one of the copies", entity, eid, recorded)
	}
	return nil
}

func IsHTML(remoteDoc map[string]any) bool {
	ct, ok := intField(remoteDoc, "content_type")
	return ok && ct == 1
}

func RefuseHTML(remoteDoc map[string]any, entity string, eid int) error {
	if IsHTML(remoteDoc) && strings.TrimSpace(strField(remoteDoc, "body")) != "" {
		opt := ""
		if entity == "items" {
			opt = " --entity items"
		}
		return HtmlEntityError{
			Msg: fmt.Sprintf("%s/%d is an HTML entity (written in the rich text editor); elport only reads those: elport view %d%s -o DIR", entity, eid, eid, opt),
		}
	}
	return nil
}

func IsHiddenUpload(u transclude.Upload) bool {
	return strings.HasPrefix(transclude.UploadName(u), ".")
}

func ReversibleUploads(uploads []transclude.Upload) []transclude.Upload {
	out := make([]transclude.Upload, 0, len(uploads))
	for _, u := range uploads {
		if !IsHiddenUpload(u) {
			out = append(out, u)
		}
	}
	return out
}

func WarnUnmatchedAttachments(body string, uploads []transclude.Upload, baseURL string, stderr io.Writer) {
	if stderr == nil {
		return
	}
	for _, name := range transclude.UnmatchedDownloadURLs(body, uploads, baseURL) {
		_, _ = fmt.Fprintf(stderr, "warning: attachment not found among this entity's uploads, kept as a URL (not downloaded, not localized): %s\n", name)
	}
}

func WarnRawHTMLOnV6(ctx context.Context, api API, body string, stderr io.Writer) {
	if stderr == nil || api == nil || !transclude.HasRawHTML(body) {
		return
	}
	info, err := api.Info(ctx)
	if err != nil || info == nil {
		return
	}
	ver := strings.TrimSpace(strField(info, "elabftw_version"))
	i := 0
	for i < len(ver) && ver[i] >= '0' && ver[i] <= '9' {
		i++
	}
	if i == 0 {
		return
	}
	major, err := strconv.Atoi(ver[:i])
	if err == nil && major >= 6 {
		_, _ = fmt.Fprintln(stderr, "warning: eLabFTW 6.x strips raw HTML from rendered Markdown; use standard Markdown such as ![alt](fig.png)")
	}
}

func SHA256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func RemoteSHA256(u transclude.Upload) string {
	if !strings.EqualFold(strField(u.Extra, "hash_algorithm"), "sha256") {
		return ""
	}
	val := strings.ToLower(strField(u.Extra, "hash"))
	if val == "" {
		val = strings.ToLower(strField(u.Extra, "sha256"))
	}
	if len(val) != 64 {
		return ""
	}
	for i := range len(val) {
		ch := val[i]
		if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f')) {
			return ""
		}
	}
	return val
}

func MatchingUpload(path string, uploads []transclude.Upload) (transclude.Upload, bool) {
	base := filepath.Base(path)
	var candidates []transclude.Upload
	for _, u := range uploads {
		if base == transclude.UploadName(u) || base == transclude.SafeName(u.RealName) {
			candidates = append(candidates, u)
		}
	}
	localHash := ""
	for _, u := range candidates {
		rh := RemoteSHA256(u)
		if rh == "" {
			continue
		}
		if localHash == "" {
			var err error
			localHash, err = SHA256File(path)
			if err != nil {
				return transclude.Upload{}, false
			}
		}
		if rh == localHash {
			return u, true
		}
	}
	return transclude.Upload{}, false
}

func SameMeta(local, base any) bool {
	if local == nil || base == nil {
		return local == nil && base == nil
	}
	return fmt.Sprint(local) == fmt.Sprint(base)
}

func EditedMeta(meta frontmatter.Meta, saved map[string]any) map[string]any {
	rawBase, ok := saved["meta_base"]
	if !ok || rawBase == nil {
		out := make(map[string]any, len(meta.Values))
		for k, v := range meta.Values {
			out[k] = v
		}
		return out
	}
	base, _ := rawBase.(map[string]any)
	syncedSet := map[string]bool{}
	for _, k := range SyncedMetaKeys {
		syncedSet[k] = true
	}
	out := map[string]any{}
	for k, v := range meta.Values {
		if !syncedSet[k] || !SameMeta(v, base[k]) {
			out[k] = v
		}
	}
	return out
}

type PermissionChange struct {
	Field   string
	Keyword string
	Target  int
	Current int
}

func PermissionChanges(meta map[string]any, remoteDoc, identity map[string]any, existing bool) []PermissionChange {
	var changes []PermissionChange
	for _, field := range []string{"read", "write"} {
		raw, ok := meta[field]
		if !ok {
			continue
		}
		kw, ok := raw.(string)
		if !ok {
			continue
		}
		target := frontmatter.PermissionLevels[kw]
		var current int
		if existing {
			current, _ = intField(remoteDoc, "can"+field+"_base")
		} else {
			current, _ = intField(identity, "default_"+field+"_base")
		}
		changes = append(changes, PermissionChange{
			Field:   field,
			Keyword: kw,
			Target:  target,
			Current: current,
		})
	}
	return changes
}

func savedStringList(saved map[string]any, key string) []string {
	if saved == nil {
		return nil
	}
	raw, ok := saved[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func savedDocLinks(saved map[string]any) map[string]map[string]string {
	if saved == nil {
		return nil
	}
	raw, ok := saved["doc_links"].(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]map[string]string, len(raw))
	for sent, val := range raw {
		fm, ok := val.(map[string]any)
		if !ok {
			continue
		}
		inner := make(map[string]string, len(fm))
		for fam, dst := range fm {
			if s, ok := dst.(string); ok {
				inner[fam] = s
			}
		}
		out[sent] = inner
	}
	return out
}
