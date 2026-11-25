package sync

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/pmezard/go-difflib/difflib"

	"github.com/youthesame/elport/internal/frontmatter"
	"github.com/youthesame/elport/internal/plan"
	"github.com/youthesame/elport/internal/render"
)

const minConfirmHashLen = 12

type pendingPublishState struct {
	TargetCT int
	User     any
	PreBody  string
	PreCT    int
}

func BodySHA256(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

func ShortHash(full string) string {
	if len(full) <= minConfirmHashLen {
		return full
	}
	return full[:minConfirmHashLen]
}

func ValidateConfirmHash(raw string) (string, error) {
	s := strings.ToLower(strings.TrimSpace(raw))
	if len(s) < minConfirmHashLen || len(s) > 64 {
		return "", fmt.Errorf("--confirm-remote must be a %d-64 character hex SHA-256 prefix", minConfirmHashLen)
	}
	for i := range len(s) {
		ch := s[i]
		if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f')) {
			return "", fmt.Errorf("--confirm-remote must be a %d-64 character hex SHA-256 prefix", minConfirmHashLen)
		}
	}
	return s, nil
}

func ConfirmMatches(confirmHex, remoteBody string) bool {
	return confirmHex != "" && strings.HasPrefix(BodySHA256(remoteBody), confirmHex)
}

func ExtractPatchBody(stored map[string]any) (string, bool) {
	if stored == nil {
		return "", false
	}
	raw, exists := stored["body"]
	if !exists {
		return "", false
	}
	if raw == nil {
		return "", true
	}
	s, ok := raw.(string)
	return s, ok
}

func DocFormat(meta frontmatter.Meta) string {
	s, _ := meta.Values["format"].(string)
	return s
}

func SavedContentType(saved map[string]any) int {
	ct, ok := intField(saved, "content_type")
	if !ok {
		return 0
	}
	return ct
}

func savedBaseContentType(saved map[string]any) int {
	if ct := SavedContentType(saved); ct == 1 || ct == 2 {
		return ct
	}
	return 2
}

func remoteContentType(remoteDoc map[string]any) int {
	if IsHTML(remoteDoc) {
		return 1
	}
	return 2
}

func HasBase(saved map[string]any) bool {
	return saved != nil && (hasKey(saved, "remote_base") || hasKey(saved, "local_base"))
}

func isConfirmedMdBase(saved map[string]any) bool {
	if !HasBase(saved) || truthyField(saved, "pending_create") {
		return false
	}
	ct := SavedContentType(saved)
	return ct == 2 || ct == 0
}

func validUserID(v any) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, x > 0
	case int64:
		return int(x), x > 0 && int64(int(x)) == x
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) || math.Trunc(x) != x || x < 1 || x > float64(math.MaxInt) {
			return 0, false
		}
		return int(x), true
	case json.Number:
		s := string(x)
		if !isAllASCIIDigits(s) {
			return 0, false
		}
		n, err := strconv.Atoi(s)
		return n, err == nil && n > 0
	case string:
		if !isAllASCIIDigits(x) {
			return 0, false
		}
		n, err := strconv.Atoi(x)
		return n, err == nil && n > 0
	default:
		return 0, false
	}
}

func sameValidUserID(a, b any) bool {
	ia, oka := validUserID(a)
	ib, okb := validUserID(b)
	return oka && okb && ia == ib
}

func IsRegisteredHTML(saved map[string]any) bool {
	return HasBase(saved) && SavedContentType(saved) == 1
}

func PendingPublish(saved map[string]any) (pendingPublishState, bool) {
	if saved == nil {
		return pendingPublishState{}, false
	}
	raw, ok := saved["pending_publish"].(map[string]any)
	if !ok || raw == nil {
		return pendingPublishState{}, false
	}
	targetCT, ok := intField(raw, "target_content_type")
	if !ok || (targetCT != 1 && targetCT != 2) {
		return pendingPublishState{}, false
	}
	preCT, _ := intField(raw, "pre_content_type")
	return pendingPublishState{
		TargetCT: targetCT,
		User:     raw["user"],
		PreBody:  strField(raw, "pre_body"),
		PreCT:    preCT,
	}, true
}

func CanonHTMLDiff(fromLabel, fromHTML, toLabel, toHTML string) (string, error) {
	ud := difflib.UnifiedDiff{
		A:        splitLinesKeepEnds(render.CanonHTML(fromHTML)),
		B:        splitLinesKeepEnds(render.CanonHTML(toHTML)),
		FromFile: fromLabel,
		ToFile:   toLabel,
		Context:  3,
	}
	return difflib.GetUnifiedDiffString(ud)
}

func writeHTMLConflictDiff(baseHTML, remoteHTML string, stderr io.Writer) {
	if stderr == nil {
		return
	}
	diffText, err := CanonHTMLDiff("base", baseHTML, "remote", remoteHTML)
	if err == nil && diffText != "" {
		_, _ = io.WriteString(stderr, diffText)
	} else if baseHTML != remoteHTML {
		_, _ = fmt.Fprintln(stderr, "note: remote HTML differs from base in formatting/markup only (canonical HTML is identical)")
	}
	_, _ = fmt.Fprintf(stderr, "remote sha256: %s\n", ShortHash(BodySHA256(remoteHTML)))
}

func remoteSourcesForPlan(remoteDoc, saved map[string]any, remoteBody RemoteBody) []plan.RemoteSource {
	sources := []plan.RemoteSource{
		{Body: string(remoteBody), ContentType: remoteContentType(remoteDoc)},
		{Body: strField(saved, "remote_base"), ContentType: savedBaseContentType(saved)},
		{Body: strField(saved, "pending_remote"), ContentType: 2},
	}
	if pp, ok := PendingPublish(saved); ok {
		sources = append(sources, plan.RemoteSource{Body: pp.PreBody, ContentType: pp.PreCT})
	}
	return sources
}

func selectedAttachmentURLs(approved *plan.Result, urls map[string]string) []string {
	if approved == nil || len(approved.Files) == 0 {
		return nil
	}
	out := make([]string, 0, len(approved.Files))
	for _, ap := range approved.Files {
		if u := urls[ap.Path()]; u != "" {
			out = append(out, u)
		}
	}
	sort.Strings(out)
	return out
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func pendingPublishMap(targetCT int, user any, preBody string, preCT int) map[string]any {
	return map[string]any{
		"target_content_type": targetCT,
		"user":                user,
		"pre_body":            preBody,
		"pre_content_type":    preCT,
	}
}

func htmlPushFailureDetail(newUploads []plan.ApprovedUploadPath, narrowed bool) string {
	var done []string
	if narrowed {
		done = append(done, "permission narrowing already applied")
	}
	if len(newUploads) > 0 {
		done = append(done, fmt.Sprintf("%d upload(s) remain on the server", len(newUploads)))
	}
	if len(done) == 0 {
		return ""
	}
	return " (" + strings.Join(done, "; ") + ")"
}
