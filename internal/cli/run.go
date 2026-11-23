package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/youthesame/elport/internal/browse"
	"github.com/youthesame/elport/internal/client"
	"github.com/youthesame/elport/internal/config"
	"github.com/youthesame/elport/internal/frontmatter"
	"github.com/youthesame/elport/internal/help"
	"github.com/youthesame/elport/internal/plan"
	"github.com/youthesame/elport/internal/sync"
	"github.com/youthesame/elport/internal/transclude"
)

type Env struct {
	WorkDir    string
	Keyring    config.Keyring
	ClientOpts []client.Option
	IsTTY      *bool
	TermWidth  int
	Now        func() time.Time
}

func (e Env) workDir() (string, error) {
	if e.WorkDir != "" {
		return e.WorkDir, nil
	}
	return os.Getwd()
}

func (e Env) keyring() config.Keyring {
	if e.Keyring != nil {
		return e.Keyring
	}
	return config.OSKeyring{}
}

func (e Env) useColor(stdout io.Writer) bool {
	if _, noColor := os.LookupEnv("NO_COLOR"); noColor {
		return false
	}
	if e.IsTTY != nil {
		return *e.IsTTY
	}
	if f, ok := stdout.(*os.File); ok {
		return term.IsTerminal(int(f.Fd()))
	}
	return false
}

func (e Env) canPrompt(stdin io.Reader, stdout io.Writer) bool {
	if e.IsTTY != nil {
		return *e.IsTTY
	}
	inFile, inOK := stdin.(*os.File)
	outFile, outOK := stdout.(*os.File)
	return inOK && outOK && term.IsTerminal(int(inFile.Fd())) && term.IsTerminal(int(outFile.Fd()))
}

func (e Env) termWidth(stdout io.Writer) int {
	if e.TermWidth > 0 {
		return e.TermWidth
	}
	if raw := os.Getenv("COLUMNS"); raw != "" {
		if cols, err := strconv.Atoi(raw); err == nil && cols > 0 {
			return cols
		}
	}
	if f, ok := stdout.(*os.File); ok {
		if w, _, err := term.GetSize(int(f.Fd())); err == nil && w > 0 {
			return w
		}
	}
	return 80
}

func (e Env) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

func colorize(text string, code int, color bool) string {
	if !color {
		return text
	}
	return fmt.Sprintf("\033[%dm%s\033[0m", code, text)
}

func readLineCtx(ctx context.Context, reader *bufio.Reader) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	type result struct {
		line string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		line, err := reader.ReadString('\n')
		ch <- result{line: line, err: err}
	}()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case res := <-ch:
		return res.line, res.err
	}
}

func readPromptSecret(ctx context.Context, stdin io.Reader, reader *bufio.Reader, promptOut io.Writer) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	_, _ = io.WriteString(promptOut, "api_key: ")
	if f, ok := stdin.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		fd := int(f.Fd())
		oldState, stateErr := term.GetState(fd)
		type result struct {
			raw []byte
			err error
		}
		ch := make(chan result, 1)
		go func() {
			raw, err := term.ReadPassword(fd)
			ch <- result{raw: raw, err: err}
		}()
		select {
		case <-ctx.Done():
			if stateErr == nil && oldState != nil {
				_ = term.Restore(fd, oldState)
			}
			_, _ = io.WriteString(promptOut, "\n")
			return "", ctx.Err()
		case res := <-ch:
			_, _ = io.WriteString(promptOut, "\n")
			if res.err != nil {
				return "", res.err
			}
			return strings.TrimSpace(string(res.raw)), nil
		}
	}
	line, err := readLineCtx(ctx, reader)
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	if errors.Is(err, io.EOF) && strings.TrimSpace(line) == "" {
		return "", errors.New("api_key is required")
	}
	return strings.TrimSpace(line), nil
}

func runLogin(ctx context.Context, cmd *Command, stdin io.Reader, stdout, stderr io.Writer, env Env) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if stdin == nil {
		return errors.New("stdin unavailable for interactive login")
	}
	reader := bufio.NewReader(stdin)
	_, _ = io.WriteString(stdout, "base_url: ")
	line, err := readLineCtx(ctx, reader)
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	baseURL := strings.TrimSpace(line)
	if baseURL == "" {
		return errors.New("base_url is required")
	}
	secret, err := readPromptSecret(ctx, stdin, reader, stderr)
	if err != nil {
		return err
	}
	if secret == "" {
		return errors.New("api_key is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return config.Login(cmd.ProfileName, baseURL, config.NewAPIKey(secret), env.keyring(), stderr)
}

func runLogout(cmd *Command, stdout io.Writer, env Env) error {
	removed, err := config.Logout(cmd.ProfileName, env.keyring())
	if err != nil {
		return err
	}
	if removed {
		_, _ = fmt.Fprintf(stdout, "logged out profile %s\n", cmd.ProfileName)
	} else {
		_, _ = fmt.Fprintf(stdout, "no stored credentials for profile %s\n", cmd.ProfileName)
	}
	return nil
}

func runProfile(cmd *Command, stdout, stderr io.Writer, env Env) error {
	wd, err := env.workDir()
	if err != nil {
		return err
	}
	data, err := config.Load(wd, wd, stderr)
	if err != nil {
		return err
	}
	profs, _ := data.Raw["profiles"].(map[string]any)
	if cmd.Action == "use" {
		if cmd.ProfileName == "" {
			return errors.New("elport profile use requires a profile name")
		}
		if _, ok := profs[cmd.ProfileName]; !ok || (data.UserProfiles != nil && !data.UserProfiles[cmd.ProfileName]) {
			return fmt.Errorf("no such profile: %s", cmd.ProfileName)
		}
		if err := config.SetDefault(cmd.ProfileName); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(stdout, "default profile is now %s\n", cmd.ProfileName)
		return nil
	}
	if len(profs) == 0 {
		_, _ = io.WriteString(stdout, "no profiles configured; run 'elport login <name>'\n")
		return nil
	}
	def, _ := data.Raw["default_profile"].(string)
	names := make([]string, 0, len(profs))
	for name := range profs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		marker := "  "
		if name == def {
			marker = "* "
		}
		baseURL := ""
		if entry, ok := profs[name].(map[string]any); ok {
			baseURL, _ = entry["base_url"].(string)
		}
		line := fmt.Sprintf("%s%s  %s", marker, name, baseURL)
		_, _ = fmt.Fprintln(stdout, strings.TrimRight(line, " "))
	}
	return nil
}

func resolveClient(data *config.Data, profile, metaProfile string, stderr io.Writer, env Env) (string, *client.Client, error) {
	name, baseURL, apiKey, verifySSL, err := config.Resolve(data, profile, metaProfile, env.keyring(), stderr)
	if err != nil {
		return "", nil, err
	}
	c, err := client.New(baseURL, apiKey, verifySSL, env.ClientOpts...)
	if err != nil {
		return "", nil, err
	}
	return name, c, nil
}

func browseTarget(cmd *Command, stderr io.Writer, env Env) (string, *client.Client, error) {
	wd, err := env.workDir()
	if err != nil {
		return "", nil, err
	}
	data, err := config.Load(wd, wd, stderr)
	if err != nil {
		return "", nil, err
	}
	entity := cmd.Entity
	if entity == "" {
		if cfgEntity, ok := data.Raw["entity"].(string); ok && cfgEntity != "" {
			entity = cfgEntity
		} else {
			entity = "experiments"
		}
	}
	if !slices.Contains(entities, entity) {
		return "", nil, errors.New("entity must be one of: experiments, items")
	}
	_, c, err := resolveClient(data, cmd.Profile, "", stderr, env)
	if err != nil {
		return "", nil, err
	}
	return entity, c, nil
}

func isASCIIAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func isRemoteTarget(text string) bool {
	if isASCIIAllDigits(text) || strings.Contains(text, "://") {
		return true
	}
	trimmed := strings.TrimLeft(text, " \t\r\n")
	low := strings.ToLower(trimmed)
	return strings.HasPrefix(trimmed, "//") || strings.HasPrefix(low, "http:") || strings.HasPrefix(low, "https:")
}

func isExistingFile(text string) (bool, error) {
	info, err := os.Stat(text)
	if err == nil {
		return info.Mode().IsRegular(), nil
	}
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) || errors.Is(err, syscall.ENAMETOOLONG) || errors.Is(err, syscall.EINVAL) {
		return false, nil
	}
	return false, errors.New("cannot inspect local target path")
}

func parseRemoteTarget(raw, explicitEntity, defaultEntity string, c *client.Client) (string, int, error) {
	if strings.Contains(raw, "://") {
		entity, eid, err := sync.ParseEntityURL(raw, c.Root())
		if err != nil {
			return "", 0, err
		}
		if explicitEntity != "" && explicitEntity != entity {
			return "", 0, fmt.Errorf("entity mismatch between URL (%s) and --entity (%s)", entity, explicitEntity)
		}
		return entity, eid, nil
	}
	num, err := strconv.Atoi(raw)
	if err != nil {
		return "", 0, errors.New("invalid entity id or URL")
	}
	eid, err := frontmatter.ValidateID(num)
	if err != nil {
		return "", 0, err
	}
	entity := explicitEntity
	if entity == "" {
		entity = defaultEntity
	}
	return entity, eid, nil
}

type lazyAPI struct {
	init   func() (*client.Client, error)
	client *client.Client
	err    error
	done   bool
}

func (l *lazyAPI) get() (*client.Client, error) {
	if !l.done {
		l.client, l.err = l.init()
		l.done = true
	}
	return l.client, l.err
}

func (l *lazyAPI) Root() string {
	c, err := l.get()
	if err != nil {
		return ""
	}
	return c.Root()
}

func (l *lazyAPI) Me(ctx context.Context) (map[string]any, error) {
	c, err := l.get()
	if err != nil {
		return nil, err
	}
	return c.Me(ctx)
}

func (l *lazyAPI) Info(ctx context.Context) (map[string]any, error) {
	c, err := l.get()
	if err != nil {
		return nil, err
	}
	return c.Info(ctx)
}

func (l *lazyAPI) Get(ctx context.Context, entity string, eid int) (map[string]any, error) {
	c, err := l.get()
	if err != nil {
		return nil, err
	}
	return c.Get(ctx, entity, eid)
}

func (l *lazyAPI) Create(ctx context.Context, entity, title string) (map[string]any, error) {
	c, err := l.get()
	if err != nil {
		return nil, err
	}
	return c.Create(ctx, entity, title)
}

func (l *lazyAPI) Patch(ctx context.Context, entity string, eid int, payload map[string]any) (map[string]any, error) {
	c, err := l.get()
	if err != nil {
		return nil, err
	}
	return c.Patch(ctx, entity, eid, payload)
}

func (l *lazyAPI) Uploads(ctx context.Context, entity string, eid int) ([]map[string]any, error) {
	c, err := l.get()
	if err != nil {
		return nil, err
	}
	return c.Uploads(ctx, entity, eid)
}

func (l *lazyAPI) Comments(ctx context.Context, entity string, eid int) ([]map[string]any, error) {
	c, err := l.get()
	if err != nil {
		return nil, err
	}
	return c.Comments(ctx, entity, eid)
}

func (l *lazyAPI) Upload(ctx context.Context, entity string, eid int, approved plan.ApprovedUploadPath) (map[string]any, error) {
	c, err := l.get()
	if err != nil {
		return nil, err
	}
	return c.Upload(ctx, entity, eid, approved)
}

func (l *lazyAPI) Download(ctx context.Context, entity string, eid, uid int) ([]byte, error) {
	c, err := l.get()
	if err != nil {
		return nil, err
	}
	return c.Download(ctx, entity, eid, uid)
}

func (l *lazyAPI) AddTag(ctx context.Context, entity string, eid int, tag string) error {
	c, err := l.get()
	if err != nil {
		return err
	}
	return c.AddTag(ctx, entity, eid, tag)
}

func (l *lazyAPI) AddLink(ctx context.Context, entity string, eid int, targetEntity string, targetID int) error {
	c, err := l.get()
	if err != nil {
		return err
	}
	return c.AddLink(ctx, entity, eid, targetEntity, targetID)
}

func (l *lazyAPI) AddComment(ctx context.Context, entity string, eid int, text string) error {
	c, err := l.get()
	if err != nil {
		return err
	}
	return c.AddComment(ctx, entity, eid, text)
}

func (l *lazyAPI) Categories(ctx context.Context, teamID any, entity string) ([]map[string]any, error) {
	c, err := l.get()
	if err != nil {
		return nil, err
	}
	return c.Categories(ctx, teamID, entity)
}

func (l *lazyAPI) Statuses(ctx context.Context, teamID any, entity string) ([]map[string]any, error) {
	c, err := l.get()
	if err != nil {
		return nil, err
	}
	return c.Statuses(ctx, teamID, entity)
}

func (l *lazyAPI) Search(ctx context.Context, entity string, params url.Values) ([]map[string]any, error) {
	c, err := l.get()
	if err != nil {
		return nil, err
	}
	return c.Search(ctx, entity, params)
}

type clientCacheKey struct {
	baseURL   string
	verifySSL bool
	profile   string
}

type clientCache struct {
	clients map[clientCacheKey]*client.Client
}

func newClientCache() *clientCache {
	return &clientCache{clients: map[clientCacheKey]*client.Client{}}
}

func resolveClientCached(data *config.Data, profile, metaProfile string, stderr io.Writer, env Env, cache *clientCache) (string, *client.Client, error) {
	if cache != nil {
		name, baseURL, verifySSL, err := config.BaseTarget(data, profile, metaProfile)
		if err == nil {
			key := clientCacheKey{baseURL: baseURL, verifySSL: verifySSL, profile: name}
			if cached, ok := cache.clients[key]; ok {
				return name, cached, nil
			}
		}
	}
	name, baseURL, apiKey, verifySSL, err := config.Resolve(data, profile, metaProfile, env.keyring(), stderr)
	if err != nil {
		return "", nil, err
	}
	c, err := client.New(baseURL, apiKey, verifySSL, env.ClientOpts...)
	if err != nil {
		return "", nil, err
	}
	if cache != nil {
		key := clientCacheKey{baseURL: baseURL, verifySSL: verifySSL, profile: name}
		cache.clients[key] = c
	}
	return name, c, nil
}

func docTarget(docPath, cliProfile, cliEntity string, stderr io.Writer, env Env, cache *clientCache) (*config.Data, sync.API, error) {
	wd, err := env.workDir()
	if err != nil {
		return nil, nil, err
	}
	data, err := config.Load(wd, filepath.Dir(docPath), stderr)
	if err != nil {
		return nil, nil, err
	}
	info, err := os.Stat(docPath)
	if err != nil || !info.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("document not found: %s", docPath)
	}
	doc, err := sync.ReadDocument(docPath, data)
	if err != nil {
		return nil, nil, err
	}
	if !slices.Contains(entities, doc.Entity) {
		return nil, nil, errors.New("entity must be one of: experiments, items")
	}
	metaEntity, _ := doc.Meta.Values["entity"].(string)
	if cliEntity != "" && metaEntity != "" && cliEntity != metaEntity {
		return nil, nil, errors.New("entity mismatch between frontmatter and CLI")
	}
	if cliEntity != "" && metaEntity == "" {
		data.Raw["entity"] = cliEntity
	}
	metaProf, _ := doc.Meta.Values["profile"].(string)
	api := &lazyAPI{
		init: func() (*client.Client, error) {
			_, c, err := resolveClientCached(data, cliProfile, metaProf, stderr, env, cache)
			return c, err
		},
	}
	return data, api, nil
}

func runView(ctx context.Context, cmd *Command, stdout, stderr io.Writer, env Env) error {
	defaultEntity, c, err := browseTarget(cmd, stderr, env)
	if err != nil {
		return err
	}
	entity, eid, err := parseRemoteTarget(cmd.ID, cmd.Entity, defaultEntity, c)
	if err != nil {
		return err
	}
	return sync.View(ctx, c, entity, eid, c.Root(), cmd.Output, stdout, stderr)
}

func runComments(ctx context.Context, cmd *Command, stdout, stderr io.Writer, env Env) error {
	remote := isRemoteTarget(cmd.Doc)
	if remote && cmd.Name == "comment" && isASCIIAllDigits(cmd.Doc) {
		exists, err := isExistingFile(cmd.Doc)
		if err != nil {
			return err
		}
		if exists {
			remote = false
		}
	}
	if remote {
		if cmd.Name == "comment" && strings.TrimSpace(cmd.Text) == "" {
			return errors.New("comment text is empty")
		}
		defaultEntity, c, err := browseTarget(cmd, stderr, env)
		if err != nil {
			return err
		}
		entity, eid, err := parseRemoteTarget(cmd.Doc, cmd.Entity, defaultEntity, c)
		if err != nil {
			return err
		}
		if cmd.Name == "comments" {
			return sync.ShowComments(ctx, c, entity, eid, stdout)
		}
		return sync.PostComment(ctx, c, entity, eid, cmd.Text, stdout)
	}
	data, c, err := docTarget(cmd.Doc, cmd.Profile, cmd.Entity, stderr, env, nil)
	if err != nil {
		return err
	}
	if cmd.Name == "comments" {
		return sync.Comments(ctx, cmd.Doc, c, data, stdout)
	}
	return sync.Comment(ctx, cmd.Doc, c, data, cmd.Text, stdout)
}

func discoverDocuments(root string) ([]string, int) {
	walkRoot := transclude.ResolveNonStrict(root)
	var found []string
	skipped := 0
	_ = filepath.WalkDir(walkRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(walkRoot, p)
		if err != nil || rel == "." {
			return nil
		}
		for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
			if strings.HasPrefix(part, ".") {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".md") || strings.HasSuffix(name, ".remote.md") || strings.HasSuffix(name, ".base.md") {
			return nil
		}
		if !d.Type().IsRegular() {
			if d.Type()&fs.ModeSymlink == 0 {
				return nil
			}
			info, statErr := os.Stat(p)
			if statErr != nil || !info.Mode().IsRegular() {
				return nil
			}
		}
		if _, ok := frontmatter.DocumentMeta(p); !ok {
			skipped++
			return nil
		}
		found = append(found, filepath.Join(root, rel))
		return nil
	})
	sort.Strings(found)
	return found, skipped
}

func expandDocPaths(paths []string) ([]string, int) {
	var found []string
	skipped := 0
	seen := map[string]bool{}
	for _, p := range paths {
		var inside []string
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			var passed int
			inside, passed = discoverDocuments(p)
			skipped += passed
		} else {
			inside = []string{p}
		}
		for _, doc := range inside {
			res := transclude.ResolveNonStrict(doc)
			if !seen[res] {
				seen[res] = true
				found = append(found, doc)
			}
		}
	}
	return found, skipped
}

func docIdent(p string) string {
	raw, err := os.ReadFile(p)
	if err != nil {
		return "experiments/-"
	}
	norm := strings.ReplaceAll(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\r", "\n")
	meta, _, err := frontmatter.Parse(norm)
	if err != nil {
		return "experiments/-"
	}
	ent, _ := meta.Values["entity"].(string)
	if ent == "" {
		ent = "experiments"
	}
	idVal, exists := meta.Values["id"]
	if !exists || idVal == nil {
		return ent + "/-"
	}
	return fmt.Sprintf("%s/%v", ent, idVal)
}

func runDocOne(ctx context.Context, cmd *Command, docPath string, stdin io.Reader, stdout, stderr io.Writer, env Env, batch bool, cache *clientCache) (string, bool, error) {
	data, c, err := docTarget(docPath, cmd.Profile, cmd.Entity, stderr, env, cache)
	if err != nil {
		return "", false, err
	}
	switch cmd.Name {
	case "push":
		wd, _ := env.workDir()
		s, err := sync.Push(ctx, docPath, c, data, sync.PushOptions{
			Profile:   cmd.Profile,
			DryRun:    cmd.DryRun,
			Force:     cmd.Force,
			AssumeYes: cmd.Yes,
			Stdin:     stdin,
			CanPrompt: !batch && env.canPrompt(stdin, stdout),
			WorkDir:   wd,
		}, stdout, stderr)
		return s, true, err
	case "pull":
		s, err := sync.Pull(ctx, docPath, c, data, cmd.Profile, stdout, stderr)
		return s, true, err
	case "status":
		s, err := sync.Status(ctx, docPath, c, data, cmd.Profile, stdout, stderr)
		return s, true, err
	case "diff":
		err := sync.Diff(ctx, docPath, c, data, cmd.Profile, cmd.Base, stdout, stderr)
		return "", false, err
	case "fetch":
		s, err := sync.Fetch(ctx, docPath, c, data, cmd.Profile, stdout, stderr)
		return s, true, err
	default:
		return "", false, fmt.Errorf("%s: not yet implemented in Go build", cmd.Name)
	}
}

func runDocCommand(ctx context.Context, cmd *Command, stdin io.Reader, stdout, stderr io.Writer, env Env) int {
	cache := newClientCache()
	if len(cmd.Docs) == 1 {
		if info, err := os.Stat(cmd.Docs[0]); err != nil || !info.IsDir() {
			if _, _, err := runDocOne(ctx, cmd, cmd.Docs[0], stdin, stdout, stderr, env, false, cache); err != nil {
				_, _ = fmt.Fprintln(stderr, err.Error())
				return ExitError
			}
			return ExitOK
		}
	}
	if cmd.Force {
		_, _ = fmt.Fprintln(stderr, "--force works on one document at a time")
		return ExitError
	}
	docs, skipped := expandDocPaths(cmd.Docs)
	if len(docs) == 0 {
		_, _ = fmt.Fprintf(stderr, "no elport documents under %s (%d skipped)\n", strings.Join(cmd.Docs, ", "), skipped)
		return ExitError
	}
	mark := "✓"
	if cmd.Name == "status" {
		mark = "*"
	}
	counts := map[string]int{}
	for _, p := range docs {
		var capOut, capErr bytes.Buffer
		summary, hasSummary, err := runDocOne(ctx, cmd, p, nil, &capOut, &capErr, env, true, cache)
		if err != nil {
			counts["✗"]++
			_, _ = fmt.Fprintf(stdout, "✗ %s  %s  %s\n", p, docIdent(p), err.Error())
		} else if hasSummary {
			m := mark
			if summary == sync.UpToDate {
				m = "·"
			}
			counts[m]++
			_, _ = fmt.Fprintf(stdout, "%s %s  %s  %s\n", m, p, docIdent(p), summary)
		}
		for _, line := range strings.Split(strings.TrimSuffix(capErr.String(), "\n"), "\n") {
			if line != "" {
				_, _ = fmt.Fprintf(stderr, "    %s\n", line)
			}
		}
		if capOut.Len() > 0 && (cmd.Name == "diff" || cmd.DryRun) {
			_, _ = fmt.Fprintf(stdout, "=== %s\n", p)
			_, _ = io.WriteString(stdout, capOut.String())
		}
	}
	var tally []string
	for _, item := range []struct {
		m string
		w string
	}{{"✓", "done"}, {"·", sync.UpToDate}, {"*", "to sync"}, {"✗", "failed"}} {
		if counts[item.m] > 0 {
			tally = append(tally, fmt.Sprintf("%d %s", counts[item.m], item.w))
		}
	}
	if skipped > 0 {
		tally = append(tally, fmt.Sprintf("%d skipped", skipped))
	}
	if len(tally) > 0 {
		_, _ = fmt.Fprintln(stdout, strings.Join(tally, ", "))
	}
	if counts["✗"] > 0 {
		return ExitError
	}
	return ExitOK
}

func strVal(m map[string]any, key string) string {
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

func intVal(m map[string]any, key string) (int, bool) {
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
		n, err := x.Int64()
		if err == nil {
			return int(n), true
		}
	case string:
		n, err := strconv.Atoi(x)
		if err == nil {
			return n, true
		}
	}
	return 0, false
}

func truthy(m map[string]any, key string) (bool, bool) {
	v, ok := m[key]
	if !ok || v == nil {
		return false, false
	}
	switch x := v.(type) {
	case bool:
		return x, true
	case int:
		return x != 0, true
	case int64:
		return x != 0, true
	case float64:
		return x != 0, true
	case json.Number:
		n, err := x.Int64()
		if err == nil {
			return n != 0, true
		}
	}
	return false, false
}

func runWhoami(ctx context.Context, cmd *Command, stdout, stderr io.Writer, env Env) error {
	wd, err := env.workDir()
	if err != nil {
		return err
	}
	data, err := config.Load(wd, wd, stderr)
	if err != nil {
		return err
	}
	profileName, c, err := resolveClient(data, cmd.Profile, "", stderr, env)
	if err != nil {
		return err
	}
	identity, err := c.Me(ctx)
	if err != nil {
		return err
	}
	server, err := c.Info(ctx)
	if err != nil {
		return err
	}
	apiKeys, err := c.APIKeys(ctx)
	if err != nil {
		return err
	}

	color := env.useColor(stdout)
	name := strVal(identity, "fullname")
	if name == "" {
		var parts []string
		if fn := strVal(identity, "firstname"); fn != "" {
			parts = append(parts, fn)
		}
		if ln := strVal(identity, "lastname"); ln != "" {
			parts = append(parts, ln)
		}
		name = strings.Join(parts, " ")
	}
	if name == "" {
		name = strVal(identity, "email")
	}
	userid := strVal(identity, "userid")
	if name == "" {
		name = userid
	}

	host := c.Root()
	if idx := strings.Index(host, "://"); idx >= 0 {
		host = host[idx+3:]
	}
	heading := fmt.Sprintf("%s %s", colorize("✓", 32, color), host)
	if name != "" {
		heading += " — " + name
	}
	if userid != "" {
		heading += fmt.Sprintf(" (uid %s)", userid)
	}
	_, _ = fmt.Fprintln(stdout, heading)
	_, _ = fmt.Fprintf(stdout, "  Profile   %s\n", profileName)

	if email := strVal(identity, "email"); email != "" {
		_, _ = fmt.Fprintf(stdout, "  Email     %s\n", email)
	}

	teamID := strVal(identity, "team")
	if rawTeams, ok := identity["teams"].([]any); ok && teamID != "" {
		for _, item := range rawTeams {
			tm, ok := item.(map[string]any)
			if !ok || strVal(tm, "id") != teamID {
				continue
			}
			teamName := strVal(tm, "name")
			if teamName == "" {
				break
			}
			role := ""
			if ug, ok := intVal(tm, "usergroup"); ok {
				switch ug {
				case 1:
					role = "sysadmin"
				case 2:
					role = "admin"
				case 4:
					role = "user"
				}
			}
			if role == "" {
				if isOwner, _ := truthy(tm, "is_owner"); isOwner {
					role = "owner"
				} else if isAdmin, _ := truthy(tm, "is_admin"); isAdmin {
					role = "admin"
				} else {
					role = "user"
				}
			}
			roleColor := map[string]int{"sysadmin": 31, "owner": 36, "admin": 35, "user": 32}[role]
			roleDisplay := colorize(role, roleColor, color)
			if isSys, _ := truthy(identity, "is_sysadmin"); isSys && role != "sysadmin" {
				roleDisplay += " · " + colorize("sysadmin", 31, color)
			}
			_, _ = fmt.Fprintf(stdout, "  Team      %s (#%s) · %s\n", teamName, teamID, roleDisplay)
			break
		}
	}

	if keyID := c.KeyIDPrefix(); keyID != "" {
		for _, item := range apiKeys {
			if strVal(item, "id") == keyID {
				if canWrite, ok := truthy(item, "can_write"); ok {
					label := "read-only"
					code := 31
					if canWrite {
						label = "read-write"
						code = 32
					}
					_, _ = fmt.Fprintf(stdout, "  API key   %s\n", colorize(label, code, color))
				}
				break
			}
		}
	}

	if ver := strVal(server, "elabftw_version"); ver != "" {
		_, _ = fmt.Fprintf(stdout, "  Server    eLabFTW %s\n", ver)
	}

	scopeExp, hasExp := intVal(identity, "scope_experiments")
	scopeItems, hasItems := intVal(identity, "scope_items")
	if hasExp && hasItems {
		scopeLabel := func(v int) string {
			switch v {
			case 1:
				return "self"
			case 2:
				return "team"
			case 3:
				return "everything"
			default:
				return strconv.Itoa(v)
			}
		}
		_, _ = fmt.Fprintf(stdout, "  Scopes    experiments=%s · items=%s\n", scopeLabel(scopeExp), scopeLabel(scopeItems))
	}
	return nil
}

func runList(ctx context.Context, cmd *Command, stdout, stderr io.Writer, env Env) error {
	entity, c, err := browseTarget(cmd, stderr, env)
	if err != nil {
		return err
	}
	var limitPtr *int
	if !cmd.All {
		limit := cmd.Limit
		limitPtr = &limit
	}
	return browse.Entities(ctx, c, browse.ListOptions{
		Entity: entity,
		Scope:  cmd.Scope,
		Limit:  limitPtr,
		Offset: cmd.Offset,
		Query:  cmd.Query,
		AsJSON: cmd.JSON,
		Order:  cmd.Order,
		Sort:   cmd.Sort,
		Tags:   cmd.Tags,
	}, stdout, stderr)
}

func runActivity(ctx context.Context, cmd *Command, stdout, stderr io.Writer, env Env) error {
	entity, c, err := browseTarget(cmd, stderr, env)
	if err != nil {
		return err
	}
	scope := "self"
	if cmd.Team {
		scope = "team"
	}
	rows, err := browse.Every(ctx, c, entity, scope, "", 0, nil)
	if err != nil {
		return err
	}
	out := browse.Activity(rows, entity, env.now(), env.useColor(stdout), env.termWidth(stdout))
	_, _ = io.WriteString(stdout, out)
	return nil
}

func RunWithEnv(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, env Env) int {
	cmd, code, err := Parse(args, stdout)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return code
	}
	if cmd == nil {
		return code
	}

	var runErr error
	switch cmd.Name {
	case "help":
		if cmd.Topic == "" {
			_, _ = io.WriteString(stdout, help.Summary())
			return ExitOK
		}
		text, _ := help.Read(cmd.Topic)
		_, _ = io.WriteString(stdout, text)
		return ExitOK
	case "login":
		runErr = runLogin(ctx, cmd, stdin, stdout, stderr, env)
	case "logout":
		runErr = runLogout(cmd, stdout, env)
	case "profile":
		runErr = runProfile(cmd, stdout, stderr, env)
	case "whoami":
		runErr = runWhoami(ctx, cmd, stdout, stderr, env)
	case "list":
		runErr = runList(ctx, cmd, stdout, stderr, env)
	case "activity":
		runErr = runActivity(ctx, cmd, stdout, stderr, env)
	case "view":
		runErr = runView(ctx, cmd, stdout, stderr, env)
	case "comments", "comment":
		runErr = runComments(ctx, cmd, stdout, stderr, env)
	case "push", "pull", "status", "diff", "fetch":
		return runDocCommand(ctx, cmd, stdin, stdout, stderr, env)
	case "merge":
		runErr = runMerge(ctx, cmd, stdout, stderr, env)
	case "new":
		runErr = runNew(ctx, cmd, stdout, stderr, env)
	case "clone":
		return runClone(ctx, cmd, stdout, stderr, env)
	default:
		runErr = fmt.Errorf("%s: not yet implemented in Go build", cmd.Name)
	}

	if runErr != nil {
		_, _ = fmt.Fprintln(stderr, runErr.Error())
		return ExitError
	}
	return ExitOK
}

func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	return RunWithEnv(context.Background(), args, stdin, stdout, stderr, Env{})
}
