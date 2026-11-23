package sync

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/youthesame/elport/internal/config"
	"github.com/youthesame/elport/internal/frontmatter"
	"github.com/youthesame/elport/internal/plan"
	"github.com/youthesame/elport/internal/state"
	"github.com/youthesame/elport/internal/transclude"
)

type PushOptions struct {
	Profile   string
	DryRun    bool
	Force     bool
	AssumeYes bool
	Stdin     io.Reader
	CanPrompt bool
	WorkDir   string
}

type docLinkTarget struct {
	baseURL string
	entity  string
	id      int
}

func resolveDocumentLinks(refs []transclude.Reference, workDir string, stderr io.Writer) (map[string]string, []docLinkTarget, error) {
	urls := map[string]string{}
	var targets []docLinkTarget
	wd := workDir
	if wd == "" {
		wd, _ = os.Getwd()
	}
	for _, ref := range refs {
		if ref.Doc == "" {
			continue
		}
		raw, err := os.ReadFile(ref.Doc)
		if err != nil {
			return nil, nil, err
		}
		normalized := strings.ReplaceAll(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\r", "\n")
		meta, _, err := frontmatter.Parse(normalized)
		if err != nil {
			return nil, nil, err
		}
		own, err := config.Load(wd, filepath.Dir(ref.Doc), stderr)
		if err != nil {
			return nil, nil, err
		}
		entity := "experiments"
		if cfgEnt, ok := own.Raw["entity"].(string); ok && cfgEnt != "" {
			entity = cfgEnt
		}
		if metaEnt, ok := meta.Values["entity"].(string); ok && metaEnt != "" {
			entity = metaEnt
		}
		if _, valid := EntityPages[entity]; !valid {
			return nil, nil, fmt.Errorf("linked document has an invalid entity: %s", ref.Path)
		}
		_, baseURL, _, err := config.BaseTarget(own, "", metaProfile(meta))
		if err != nil {
			return nil, nil, err
		}
		targetID, err := frontmatter.ValidateID(meta.Values["id"])
		if err != nil {
			return nil, nil, err
		}
		urls[ref.Doc] = EntityURL(baseURL, entity, targetID)
		targets = append(targets, docLinkTarget{
			baseURL: strings.TrimRight(baseURL, "/"),
			entity:  entity,
			id:      targetID,
		})
	}
	return urls, targets, nil
}

func writtenLinks(refs []transclude.Reference, body string, urls map[string]string) map[string]map[string]string {
	record := map[string]map[string]string{}
	for _, ref := range refs {
		target := ref.File
		if target == "" {
			target = ref.Doc
		}
		if target == "" {
			continue
		}
		family := "markdown"
		if ref.Kind == "html" {
			family = "html"
		}
		sent := urls[target] + ref.Fragment
		if record[sent] == nil {
			record[sent] = map[string]string{}
		}
		record[sent][family] = body[ref.Start:ref.End]
	}
	return record
}

func confirmPrompt(ctx context.Context, what, detail, question string, opts PushOptions, stdout io.Writer) error {
	if opts.AssumeYes {
		return nil
	}
	if !opts.CanPrompt || opts.Stdin == nil {
		return fmt.Errorf("%s requires confirmation (%s); re-run with --yes only if that is intended", what, detail)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "%s [y/N] ", question)
	type readResult struct {
		line string
		err  error
	}
	ch := make(chan readResult, 1)
	go func() {
		reader := bufio.NewReader(opts.Stdin)
		line, err := reader.ReadString('\n')
		ch <- readResult{line: line, err: err}
	}()
	var answer string
	select {
	case <-ctx.Done():
		return ctx.Err()
	case res := <-ch:
		if res.err != nil && !errors.Is(res.err, io.EOF) {
			return res.err
		}
		answer = strings.ToLower(strings.TrimSpace(res.line))
	}
	if answer != "y" && answer != "yes" {
		return fmt.Errorf("%s cancelled", what)
	}
	return nil
}

func confirmLargeUploads(ctx context.Context, paths []plan.ApprovedUploadPath, opts PushOptions, stdout io.Writer) error {
	var large []string
	for _, ap := range paths {
		info, err := os.Stat(ap.Path())
		if err != nil {
			return err
		}
		if info.Size() > LargeUploadBytes {
			large = append(large, fmt.Sprintf("%s (%d bytes)", filepath.Base(ap.Path()), info.Size()))
		}
	}
	if len(large) == 0 {
		return nil
	}
	names := strings.Join(large, ", ")
	return confirmPrompt(ctx, "large upload", names, fmt.Sprintf("upload large file(s) %s?", names), opts, stdout)
}

func confirmPermissionWidening(ctx context.Context, changes []PermissionChange, opts PushOptions, stdout io.Writer) error {
	var parts []string
	for _, ch := range changes {
		if ch.Target >= 40 && ch.Target > ch.Current {
			parts = append(parts, fmt.Sprintf("%s → %s (%s)", ch.Field, ch.Keyword, permissionAudiences[ch.Keyword]))
		}
	}
	if len(parts) == 0 {
		return nil
	}
	summary := strings.Join(parts, ", ")
	return confirmPrompt(ctx, "permission widening", summary, fmt.Sprintf("%s: leave the team?", summary), opts, stdout)
}

func hasIndividualGrants(remoteDoc map[string]any, field string) bool {
	raw, ok := remoteDoc["can"+field]
	if !ok || raw == nil {
		return false
	}
	var grants map[string]any
	switch v := raw.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			return false
		}
		if err := json.Unmarshal([]byte(v), &grants); err != nil {
			return false
		}
	case map[string]any:
		grants = v
	default:
		return false
	}
	for _, k := range []string{"teams", "users", "teamgroups"} {
		if list, ok := grants[k].([]any); ok && len(list) > 0 {
			return true
		}
	}
	return false
}

func warnPermissionNarrowing(docPath string, changes []PermissionChange, remoteDoc map[string]any, stderr io.Writer) {
	if stderr == nil {
		return
	}
	for _, ch := range changes {
		if ch.Target <= 20 && hasIndividualGrants(remoteDoc, ch.Field) {
			_, _ = fmt.Fprintf(stderr, "warning: %s declares %s: %s but individual grants remain (managed in the Web UI); effective access is wider\n", filepath.Base(docPath), ch.Field, ch.Keyword)
		}
	}
}

func uploadMapComplete(m map[string]any) bool {
	if m == nil {
		return false
	}
	_, hasLong := m["long_name"]
	_, hasReal := m["real_name"]
	_, hasStorage := m["storage"]
	return hasLong && hasReal && hasStorage
}

func completeUploads(ctx context.Context, remote *Remote, rawUploaded map[string]map[string]any) (map[string]transclude.Upload, error) {
	if len(rawUploaded) == 0 {
		return map[string]transclude.Upload{}, nil
	}
	allComplete := true
	for _, m := range rawUploaded {
		if !uploadMapComplete(m) {
			allComplete = false
			break
		}
	}
	var listedByID map[string]transclude.Upload
	if !allComplete {
		listed, err := remote.Uploads(ctx, nil)
		if err != nil {
			return nil, err
		}
		listedByID = make(map[string]transclude.Upload, len(listed))
		for _, u := range listed {
			listedByID[strconv.Itoa(u.ID)] = u
		}
	}
	result := make(map[string]transclude.Upload, len(rawUploaded))
	for absPath, m := range rawUploaded {
		if uploadMapComplete(m) {
			result[absPath] = uploadFromMap(m)
			continue
		}
		idStr := strField(m, "id")
		found, ok := listedByID[idStr]
		if !ok {
			return nil, errors.New("uploaded file metadata could not be retrieved")
		}
		result[absPath] = found
	}
	return result, nil
}

func checkUnresolvedMergeMarkers(body string) bool {
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "<<<<<<< ") || strings.HasPrefix(line, ">>>>>>> ") {
			return true
		}
	}
	return false
}

func sameIdentityVal(a, b any) bool {
	if a == nil || b == nil {
		return false
	}
	if ia, oka := anyToInt(a); oka {
		if ib, okb := anyToInt(b); okb {
			return ia == ib
		}
	}
	return fmt.Sprint(a) == fmt.Sprint(b)
}

func isTeamAdminFor(identity map[string]any, teamVal any) bool {
	if identity == nil || teamVal == nil {
		return false
	}
	rawTeams, ok := identity["teams"].([]any)
	if !ok {
		return false
	}
	for _, item := range rawTeams {
		tm, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if sameIdentityVal(tm["id"], teamVal) && truthyField(tm, "is_admin") {
			return true
		}
	}
	return false
}

func Push(ctx context.Context, docPath string, api API, cfg *config.Data, opts PushOptions, stdout, stderr io.Writer) (string, error) {
	doc, err := ReadDocument(docPath, cfg)
	if err != nil {
		return "", err
	}
	if !opts.Force {
		if checkUnresolvedMergeMarkers(string(doc.Body)) {
			return "", fmt.Errorf("unresolved merge markers in %s; resolve them or use --force", docPath)
		}
		if strings.TrimSpace(string(doc.Body)) == "" {
			return "", errors.New("refusing to push an empty body; use --force to overwrite with an empty body")
		}
	}

	meta := doc.Meta.Clone()
	for _, key := range []string{"entity", "category", "status"} {
		if v, exists := meta.Values[key]; exists && v != nil {
			meta.Set(key, fmt.Sprint(v))
		}
	}
	if meta.Has("tags") {
		meta.Set("tags", metaTags(meta))
	}
	metaBase := syncedMeta(meta)
	resolvedProfile, baseURL, _, err := config.BaseTarget(cfg, opts.Profile, metaProfile(meta))
	if err != nil {
		return "", err
	}

	ignore := ignoreLayers(cfg)
	localPlan, err := plan.LocalCandidates(string(doc.Body), filepath.Dir(docPath), ignore, metaAttachments(meta), stderr)
	if err != nil {
		return "", err
	}
	linkURLs, linkTargets, err := resolveDocumentLinks(localPlan.DocRefs(), opts.WorkDir, stderr)
	if err != nil {
		return "", err
	}

	var remote *Remote
	var saved map[string]any
	if doc.HasID {
		remote = &Remote{API: api, Entity: doc.Entity, ID: doc.ID, BaseURL: baseURL}
		saved, err = state.Load(baseURL, doc.Entity, strconv.Itoa(doc.ID))
		if err != nil {
			return "", err
		}
		if err := CheckWorkingCopy(saved, docPath, doc.Entity, doc.ID); err != nil {
			return "", err
		}
	}

	identity, err := api.Me(ctx)
	if err != nil {
		return "", err
	}
	edited := EditedMeta(meta, saved)
	remoteDoc := map[string]any{}
	if remote != nil {
		remoteDoc, err = remote.Get(ctx)
		if err != nil {
			return "", err
		}
	}

	team := remoteDoc["team"]
	if team == nil || fmt.Sprint(team) == "" || fmt.Sprint(team) == "0" {
		team = identity["team"]
	}

	var categoryID int
	hasCategory := false
	if rawCat, ok := edited["category"]; ok && rawCat != nil {
		entries, err := api.Categories(ctx, team, doc.Entity)
		if err != nil {
			return "", err
		}
		categoryID, err = resolveCatalogID(entries, rawCat, "category")
		if err != nil {
			return "", err
		}
		hasCategory = true
	}

	var statusID int
	hasStatus := false
	if rawStat, ok := edited["status"]; ok && rawStat != nil {
		entries, err := api.Statuses(ctx, team, doc.Entity)
		if err != nil {
			return "", err
		}
		statusID, err = resolveCatalogID(entries, rawStat, "status")
		if err != nil {
			return "", err
		}
		hasStatus = true
	}

	if err := RefuseHTML(remoteDoc, doc.Entity, doc.ID); err != nil {
		return "", err
	}
	if truthyField(remoteDoc, "locked") {
		return "", fmt.Errorf("%s/%d is locked; unlock it in the Web UI to push (--force does not override a lock)", doc.Entity, doc.ID)
	}

	permChanges := PermissionChanges(edited, remoteDoc, identity, doc.HasID)
	isOwner := sameIdentityVal(remoteDoc["userid"], identity["userid"])
	isTeamAdmin := isTeamAdminFor(identity, remoteDoc["team"])
	for _, ch := range permChanges {
		if ch.Target != ch.Current && (truthyField(remoteDoc, "can"+ch.Field+"_is_immutable") || truthyField(remoteDoc, "can"+ch.Field+"_base_is_immutable")) {
			return "", fmt.Errorf("%s permission is locked on this entity (admin-set); remove '%s:' or ask an admin", ch.Field, ch.Field)
		}
		if ch.Target == frontmatter.PermissionLevels["owner"] && ch.Target < ch.Current && doc.HasID && !isOwner {
			return "", fmt.Errorf("%s: owner would revoke your own access (you are not the entity owner); use 'team' or ask the owner", ch.Field)
		}
		if ch.Target == frontmatter.PermissionLevels["owner+admin"] && ch.Target < ch.Current && doc.HasID && !(isOwner || isTeamAdmin) {
			return "", fmt.Errorf("%s: owner+admin would revoke your own access (you are not an owner or admin of this team); use 'team' or ask an admin", ch.Field)
		}
	}

	if doc.HasID && saved == nil && !opts.Force {
		return "", errors.New("base unavailable on this machine; run elport pull first. --force overwrites the remote without comparing")
	}

	resuming := saved != nil && truthyField(saved, "pending_create") && truthyField(saved, "body_sent") && !hasKey(saved, "pending_remote")
	remoteBody := RemoteBody(strField(remoteDoc, "body"))
	savedRemoteBase := RemoteBody(strField(saved, "remote_base"))

	if remote != nil && saved != nil && !resuming && remoteBody != savedRemoteBase && !opts.Force {
		msg := "remote changed (edited in the Web UI?); run elport pull, then elport merge. --force discards the remote edit"
		if opts.DryRun {
			return "", errors.New(msg)
		}
		uploads, err := remote.Uploads(ctx, remoteDoc)
		if err != nil {
			return "", err
		}
		return "", raiseConflict(ctx, docPath, remoteBody, saved, uploads, remote, msg, stderr)
	}
	if opts.Force && remote != nil && saved != nil && !resuming && remoteBody != savedRemoteBase && !opts.DryRun && stderr != nil {
		_, _ = fmt.Fprintln(stderr, "warning: --force is discarding a remote change (the Web-side edit will be lost)")
	}
	if resuming && !opts.DryRun && remoteBody != "" && stderr != nil {
		_, _ = fmt.Fprintln(stderr, "warning: resuming an interrupted creation; the remote body is being overwritten")
	}
	if !opts.DryRun {
		if err := confirmPermissionWidening(ctx, permChanges, opts, stdout); err != nil {
			return "", err
		}
		if doc.HasID {
			warnPermissionNarrowing(docPath, permChanges, remoteDoc, stderr)
		}
	}

	approved, err := plan.ApproveFromLocal(
		localPlan,
		filepath.Dir(docPath),
		ignore,
		[]string{string(remoteBody), strField(saved, "remote_base"), strField(saved, "pending_remote")},
		savedStringList(saved, "remote_paths"),
		stderr,
	)
	if err != nil {
		return "", err
	}

	var uploads []transclude.Upload
	if remote != nil && len(approved.Files) > 0 {
		uploads, err = remote.Uploads(ctx, remoteDoc)
		if err != nil {
			return "", err
		}
	}
	reused := map[string]transclude.Upload{}
	var newUploads []plan.ApprovedUploadPath
	for _, ap := range approved.Files {
		if matched, ok := MatchingUpload(ap.Path(), uploads); ok {
			reused[ap.Path()] = matched
		} else {
			newUploads = append(newUploads, ap)
		}
	}

	if opts.DryRun {
		parts := make([]string, 0, 1+len(approved.Files))
		parts = append(parts, "Upload plan:")
		for _, ap := range approved.Files {
			parts = append(parts, filepath.Base(ap.Path()))
		}
		_, _ = fmt.Fprintln(stdout, strings.Join(parts, "\n  "))
		if len(permChanges) > 0 {
			planned := make([]string, len(permChanges))
			for i, ch := range permChanges {
				planned[i] = fmt.Sprintf("%s→%s", ch.Field, ch.Keyword)
			}
			_, _ = fmt.Fprintf(stdout, "permissions: %s\n", strings.Join(planned, ", "))
		}
		if title := strField(edited, "title"); title != "" {
			_, _ = fmt.Fprintf(stdout, "title: %s\n", title)
		}
		if hasCategory {
			_, _ = fmt.Fprintf(stdout, "category: %d\n", categoryID)
		}
		if rawStat, ok := edited["status"]; ok && rawStat != nil {
			_, _ = fmt.Fprintf(stdout, "status: %v\n", rawStat)
		}
		existing := remoteTags(remoteDoc)
		var newTags []string
		for _, tag := range metaTags(meta) {
			if !existing[tag] {
				newTags = append(newTags, tag)
			}
		}
		if len(newTags) > 0 {
			_, _ = fmt.Fprintf(stdout, "tags +: %s\n", strings.Join(newTags, ", "))
		}
		for _, ref := range approved.Refs {
			if ref.File == "" {
				continue
			}
			var target string
			if u, ok := reused[ref.File]; ok {
				target = transclude.DownloadURL(baseURL, u)
			} else {
				target = "UPLOAD_PENDING:" + filepath.Base(ref.File)
			}
			target += ref.Fragment
			_, _ = fmt.Fprintf(stdout, "%s -> %s\n", ref.Path, target)
		}
		for _, ref := range approved.Refs {
			if ref.Doc != "" {
				_, _ = fmt.Fprintf(stdout, "%s -> %s%s\n", ref.Path, linkURLs[ref.Doc], ref.Fragment)
			}
		}
		WarnRawHTMLOnV6(ctx, api, string(doc.Body), stderr)
		return "dry run", nil
	}

	if err := confirmLargeUploads(ctx, newUploads, opts, stdout); err != nil {
		return "", err
	}

	pendingCreate := saved != nil && truthyField(saved, "pending_create")
	eid := doc.ID
	if !doc.HasID {
		title := strField(meta.Values, "title")
		if title == "" {
			title = PathStem(filepath.Base(docPath))
		}
		created, err := api.Create(ctx, doc.Entity, title)
		if err != nil {
			return "", err
		}
		eid, err = frontmatter.ParseServerID(created["id"])
		if err != nil {
			return "", err
		}
		meta.Set("id", eid)
		meta.Set("entity", doc.Entity)
		meta.Set("profile", resolvedProfile)
		if err := AtomicWriteDoc(docPath, frontmatter.Render(meta, string(doc.Body))); err != nil {
			return "", err
		}
		if err := MarkCreated(baseURL, doc.Entity, eid, false); err != nil {
			return "", err
		}
		pendingCreate = true
	}
	remote = &Remote{API: api, Entity: doc.Entity, ID: eid, BaseURL: baseURL}

	narrowing := map[string]any{}
	for _, ch := range permChanges {
		if ch.Target < ch.Current {
			narrowing["can"+ch.Field+"_base"] = ch.Target
		}
	}
	if len(narrowing) > 0 {
		if _, err := remote.Patch(ctx, narrowing); err != nil {
			return "", err
		}
	}

	rawUploaded := make(map[string]map[string]any, len(newUploads))
	for _, ap := range newUploads {
		u, err := remote.Upload(ctx, ap)
		if err != nil {
			return "", err
		}
		rawUploaded[ap.Path()] = u
	}
	uploaded, err := completeUploads(ctx, remote, rawUploaded)
	if err != nil {
		return "", err
	}

	urls := make(map[string]string, len(approved.Files)+len(linkURLs))
	for _, ap := range approved.Files {
		u, ok := reused[ap.Path()]
		if !ok {
			u = uploaded[ap.Path()]
		}
		urls[ap.Path()] = transclude.DownloadURL(remote.BaseURL, u)
	}
	for k, v := range linkURLs {
		urls[k] = v
	}
	docLinks := writtenLinks(approved.Refs, string(doc.Body), urls)

	sent := transclude.ReplaceSpans(string(doc.Body), approved.Refs, urls)
	payload := map[string]any{
		"body":         sent,
		"content_type": 2,
	}
	if title := strField(edited, "title"); title != "" {
		payload["title"] = title
	}
	if hasCategory {
		payload["category"] = categoryID
	}
	if hasStatus {
		payload["status"] = statusID
	}
	moved := false
	for _, ch := range permChanges {
		if ch.Target != ch.Current {
			payload["can"+ch.Field+"_base"] = ch.Target
			moved = true
		}
	}

	payloadMatchesRemote := true
	for k, v := range payload {
		if k == "body" {
			continue
		}
		if !remoteValEqual(remoteDoc[k], v) {
			payloadMatchesRemote = false
			break
		}
	}
	unchanged := saved != nil &&
		!pendingCreate &&
		doc.Body == LocalBody(strField(saved, "local_base")) &&
		remoteBody == savedRemoteBase &&
		docLinksEqual(docLinks, savedDocLinks(saved)) &&
		len(newUploads) == 0 &&
		len(transclude.UnmatchedDownloadURLs(string(remoteBody), uploads, baseURL)) == 0 &&
		payloadMatchesRemote

	if saved != nil && !opts.Force && !unchanged && (len(approved.Files) > 0 || moved) {
		reference := savedRemoteBase
		if resuming {
			reference = remoteBody
		}
		latest, err := remote.Get(ctx)
		if err != nil {
			return "", err
		}
		latestBody := RemoteBody(strField(latest, "body"))
		if latestBody != reference {
			latestUploads, err := remote.Uploads(ctx, latest)
			if err != nil {
				return "", err
			}
			return "", raiseConflict(ctx, docPath, latestBody, saved, latestUploads, remote, "remote changed after uploads; body was not updated and uploads remain", stderr)
		}
	}

	if pendingCreate {
		if err := MarkCreated(baseURL, doc.Entity, eid, true); err != nil {
			return "", err
		}
	}
	stored := remoteDoc
	if !unchanged {
		stored, err = remote.Patch(ctx, payload)
		if err != nil {
			return "", err
		}
	}
	if ct, ok := intField(stored, "content_type"); !ok || ct != 2 {
		return "", errors.New("the server kept this entity out of markdown mode (eLabFTW 5.3.11 to 5.5.2 ignore content_type); upgrade eLabFTW")
	}

	registered := savedLinks(saved)
	record := map[string]any{
		"remote_base": strField(stored, "body"),
		"local_base":  string(doc.Body),
		"meta_base":   metaBase,
		"path":        transclude.ResolveNonStrict(docPath),
	}
	if len(docLinks) > 0 {
		record["doc_links"] = docLinks
	}
	if len(registered) > 0 {
		record["links"] = registered
	}
	remembered := rememberRemotePaths(saved, strField(saved, "remote_base"), strField(saved, "pending_remote"), string(remoteBody))
	if len(remembered) > 0 {
		record["remote_paths"] = remembered
	}
	if err := state.Save(baseURL, doc.Entity, strconv.Itoa(eid), record); err != nil {
		return "", err
	}
	if !unchanged {
		WarnRawHTMLOnV6(ctx, api, string(doc.Body), stderr)
	}

	existingTags := remoteTags(remoteDoc)
	for _, tag := range metaTags(meta) {
		if !existingTags[tag] {
			if err := api.AddTag(ctx, doc.Entity, eid, tag); err != nil {
				return "", err
			}
			existingTags[tag] = true
		}
	}

	hasLink := func(list [][]any, ent string, id int) bool {
		for _, pair := range list {
			if len(pair) == 2 {
				if pe, ok := pair[0].(string); ok && pe == ent {
					if pid, ok := anyToInt(pair[1]); ok && pid == id {
						return true
					}
				}
			}
		}
		return false
	}

	normBase := strings.TrimRight(baseURL, "/")
	var newLinks [][]any
	for _, lt := range linkTargets {
		if lt.baseURL != normBase || (lt.entity == doc.Entity && lt.id == eid) {
			continue
		}
		if hasLink(registered, lt.entity, lt.id) || hasLink(newLinks, lt.entity, lt.id) {
			continue
		}
		if err := api.AddLink(ctx, doc.Entity, eid, lt.entity, lt.id); err != nil {
			return "", err
		}
		newLinks = append(newLinks, []any{lt.entity, lt.id})
		allLinks := make([][]any, 0, len(registered)+len(newLinks))
		allLinks = append(allLinks, registered...)
		allLinks = append(allLinks, newLinks...)
		updated := make(map[string]any, len(record)+1)
		for k, v := range record {
			updated[k] = v
		}
		updated["links"] = allLinks
		if err := state.Save(baseURL, doc.Entity, strconv.Itoa(eid), updated); err != nil {
			return "", err
		}
	}

	title := strField(meta.Values, "title")
	if title == "" {
		title = PathStem(filepath.Base(docPath))
	}
	bodyChanged := saved == nil || doc.Body != LocalBody(strField(saved, "local_base"))
	reusedCount := len(approved.Files) - len(newUploads)
	newCount := len(newUploads)
	_, _ = fmt.Fprintf(stdout, "pushed %s/%d: %s\n", doc.Entity, eid, title)
	if bodyChanged {
		_, _ = fmt.Fprintln(stdout, "  body updated (markdown)")
	} else {
		_, _ = fmt.Fprintln(stdout, "  body unchanged (markdown)")
	}
	named := ""
	if newCount > 0 {
		newBaseNames := make([]string, newCount)
		for i, ap := range newUploads {
			newBaseNames[i] = filepath.Base(ap.Path())
		}
		named = fmt.Sprintf(" (%s)", strings.Join(newBaseNames, ", "))
	}
	_, _ = fmt.Fprintf(stdout, "  uploads: %d reused, %d new%s\n", reusedCount, newCount, named)
	if len(newLinks) > 0 {
		_, _ = fmt.Fprintf(stdout, "  links: %d new\n", len(newLinks))
	}
	if len(permChanges) > 0 {
		applied := make([]string, len(permChanges))
		for i, ch := range permChanges {
			applied[i] = fmt.Sprintf("%s=%s", ch.Field, ch.Keyword)
		}
		_, _ = fmt.Fprintf(stdout, "  permissions: %s\n", strings.Join(applied, " "))
	}
	if shareURL := strField(stored, "sharelink"); shareURL != "" {
		_, _ = fmt.Fprintf(stdout, "  → %s\n", shareURL)
	}
	var details []string
	if bodyChanged {
		details = append(details, "body updated")
	} else {
		details = append(details, "body unchanged")
	}
	if newCount > 0 {
		details = append(details, fmt.Sprintf("%d new upload(s)", newCount))
	}
	if len(newLinks) > 0 {
		details = append(details, fmt.Sprintf("%d new link(s)", len(newLinks)))
	}
	return fmt.Sprintf("pushed (%s)", strings.Join(details, ", ")), nil
}
