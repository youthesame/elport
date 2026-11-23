package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/text/unicode/norm"

	"github.com/youthesame/elport/internal/browse"
	"github.com/youthesame/elport/internal/client"
	"github.com/youthesame/elport/internal/config"
	"github.com/youthesame/elport/internal/frontmatter"
	"github.com/youthesame/elport/internal/state"
	"github.com/youthesame/elport/internal/sync"
	"github.com/youthesame/elport/internal/transclude"
)

const titleLimit = 50

func DirectoryName(eid int, title string) string {
	s := norm.NFC.String(title)
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r < 0x20 || r == 0x7f || strings.ContainsRune(`/\:*?"<>|`, r) {
			b.WriteByte('_')
		} else {
			b.WriteRune(r)
		}
	}
	cleaned := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(b.String()), "."))
	runes := []rune(cleaned)
	if len(runes) > titleLimit {
		runes = runes[:titleLimit]
	}
	name := strings.TrimRight(string(runes), " .")
	if name == "" {
		return strconv.Itoa(eid)
	}
	return fmt.Sprintf("%d-%s", eid, name)
}

func whereSaved(saved map[string]any) string {
	if p := strVal(saved, "path"); p != "" {
		return p
	}
	return "an unrecorded path"
}

func runNew(ctx context.Context, cmd *Command, stdout, stderr io.Writer, env Env) error {
	outPath := cmd.Output
	if _, err := os.Lstat(outPath); err == nil {
		return fmt.Errorf("output already exists: %s\nto create an entity from an existing note, run: elport push %s", outPath, outPath)
	}
	parent := filepath.Dir(outPath)
	if info, err := os.Stat(parent); err != nil || !info.IsDir() {
		return fmt.Errorf("output parent does not exist: %s", parent)
	}
	wd, err := env.workDir()
	if err != nil {
		return err
	}
	data, err := config.Load(wd, parent, stderr)
	if err != nil {
		return err
	}
	resolvedProfile, c, err := resolveClient(data, cmd.Profile, "", stderr, env)
	if err != nil {
		return err
	}
	meta := frontmatter.NewMeta()
	meta.Set("title", cmd.Title)
	meta.Set("entity", cmd.Entity)
	meta.Set("profile", resolvedProfile)
	if err := sync.WriteNewDocNoClobber(outPath, frontmatter.Render(meta, "")); err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("output already exists: %s\nto create an entity from an existing note, run: elport push %s", outPath, outPath)
		}
		return err
	}
	created, err := c.Create(ctx, cmd.Entity, cmd.Title)
	if err != nil {
		return err
	}
	eid, err := frontmatter.ParseServerID(created["id"])
	if err != nil {
		return err
	}
	meta.Set("id", eid)
	if err := sync.AtomicWriteDoc(outPath, frontmatter.Render(meta, "")); err != nil {
		return err
	}
	if err := sync.MarkCreated(c.Root(), cmd.Entity, eid, false); err != nil {
		return err
	}
	remoteDoc, err := c.Get(ctx, cmd.Entity, eid)
	if err != nil {
		return err
	}
	if err := state.Save(c.Root(), cmd.Entity, strconv.Itoa(eid), map[string]any{
		"remote_base": strVal(remoteDoc, "body"),
		"local_base":  "",
		"path":        transclude.ResolveNonStrict(outPath),
	}); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "created %s/%d: %s\n", cmd.Entity, eid, outPath)
	if shareURL := strVal(remoteDoc, "sharelink"); shareURL != "" {
		_, _ = fmt.Fprintf(stdout, "  → %s\n", shareURL)
	}
	return nil
}

func cloneTo(
	ctx context.Context,
	target, explicitEntity, outPath, profile string,
	existingClient *client.Client,
	existingData *config.Data,
	existingProfile string,
	stdout, stderr io.Writer,
	env Env,
) (string, error) {
	if _, err := os.Lstat(outPath); err == nil {
		return "", fmt.Errorf("output already exists: %s", outPath)
	}
	parent := filepath.Dir(outPath)
	if info, err := os.Stat(parent); err != nil || !info.IsDir() {
		return "", fmt.Errorf("output parent does not exist: %s", parent)
	}
	data := existingData
	c := existingClient
	resolvedProfile := existingProfile
	if c == nil || data == nil {
		wd, err := env.workDir()
		if err != nil {
			return "", err
		}
		data, err = config.Load(wd, parent, stderr)
		if err != nil {
			return "", err
		}
		resolvedProfile, c, err = resolveClient(data, profile, "", stderr, env)
		if err != nil {
			return "", err
		}
	}
	entity, eid, err := parseRemoteTarget(target, explicitEntity, "experiments", c)
	if err != nil {
		return "", err
	}
	basePath, err := state.Path(c.Root(), entity, strconv.Itoa(eid))
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(basePath); err == nil {
		saved, _ := state.Load(c.Root(), entity, strconv.Itoa(eid))
		return "", fmt.Errorf("%s/%d already has a working copy at %s; pull there instead (to start over, delete its base: %s)", entity, eid, whereSaved(saved), basePath)
	}
	meta := frontmatter.NewMeta()
	meta.Set("id", eid)
	meta.Set("entity", entity)
	meta.Set("profile", resolvedProfile)
	if err := sync.WriteNewDocNoClobber(outPath, frontmatter.Render(meta, "")); err != nil {
		if errors.Is(err, os.ErrExist) {
			return "", fmt.Errorf("output already exists: %s", outPath)
		}
		return "", err
	}
	if _, err := sync.Pull(ctx, outPath, c, data, profile, stdout, stderr); err != nil {
		if _, serr := os.Stat(basePath); errors.Is(serr, os.ErrNotExist) {
			_ = os.Remove(outPath)
		}
		return "", err
	}
	return "cloned", nil
}

type presentKey struct {
	profile string
	entity  string
	id      int
}

func runCloneAll(ctx context.Context, cmd *Command, stdout, stderr io.Writer, env Env) int {
	root := cmd.AllRoot
	if root == "" {
		root = "."
	}
	wd, err := env.workDir()
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return ExitError
	}
	data, err := config.Load(wd, root, stderr)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return ExitError
	}
	profileName, c, err := resolveClient(data, cmd.Profile, "", stderr, env)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return ExitError
	}

	present := map[presentKey]bool{}
	if info, err := os.Stat(root); err == nil && info.IsDir() {
		docs, _ := discoverDocuments(root)
		for _, p := range docs {
			meta, ok := frontmatter.DocumentMeta(p)
			if !ok {
				continue
			}
			ent, _ := meta.Values["entity"].(string)
			if ent == "" {
				ent = "experiments"
			}
			prof, _ := meta.Values["profile"].(string)
			id, err := frontmatter.ValidateID(meta.Values["id"])
			if err != nil {
				continue
			}
			present[presentKey{profile: prof, entity: ent, id: id}] = true
		}
	}

	targetEntities := entities
	if cmd.Entity != "" {
		targetEntities = []string{cmd.Entity}
	}

	counts := map[string]int{}
	interrupted := false
	for _, entity := range targetEntities {
		if ctx.Err() != nil {
			interrupted = true
			break
		}
		folder := filepath.Join(root, entity)
		taken := map[string]bool{}
		if entries, err := os.ReadDir(folder); err == nil {
			for _, de := range entries {
				if de.IsDir() {
					taken[de.Name()] = true
				}
			}
		}
		rows, err := browse.Every(ctx, c, entity, cmd.Scope, "", 0, nil)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, err.Error())
			return ExitError
		}
		for _, row := range rows {
			if ctx.Err() != nil {
				interrupted = true
				break
			}
			eid, err := frontmatter.ParseServerID(row["id"])
			if err != nil {
				_, _ = fmt.Fprintln(stderr, err.Error())
				return ExitError
			}
			eidStr := strconv.Itoa(eid)
			prefix := eidStr + "-"
			alreadyTaken := present[presentKey{profile: profileName, entity: entity, id: eid}]
			if !alreadyTaken {
				for name := range taken {
					if name == eidStr || strings.HasPrefix(name, prefix) {
						alreadyTaken = true
						break
					}
				}
			}
			if alreadyTaken {
				counts["·"]++
				continue
			}
			saved, err := state.Load(c.Root(), entity, eidStr)
			if err != nil {
				_, _ = fmt.Fprintln(stderr, err.Error())
				return ExitError
			}
			if saved != nil {
				counts["·"]++
				_, _ = fmt.Fprintf(stdout, "· %s/%d  working copy at %s\n", entity, eid, whereSaved(saved))
				continue
			}

			dirName := DirectoryName(eid, strVal(row, "title"))
			directory := filepath.Join(root, entity, dirName)
			docPath := filepath.Join(directory, "report.md")
			basePath, _ := state.Path(c.Root(), entity, eidStr)

			var capOut, capErr bytes.Buffer
			summary, workErr := func() (string, error) {
				if err := os.MkdirAll(filepath.Dir(directory), 0o755); err != nil {
					return "", err
				}
				if err := os.Mkdir(directory, 0o755); err != nil {
					return "", err
				}
				res, err := cloneTo(ctx, eidStr, entity, docPath, profileName, c, data, profileName, &capOut, &capErr, env)
				if err != nil {
					var htmlErr sync.HtmlEntityError
					if errors.As(err, &htmlErr) {
						if _, serr := sync.Snapshot(ctx, c, entity, eid, directory, c.Root(), &capErr); serr != nil {
							if _, berr := os.Stat(basePath); errors.Is(berr, os.ErrNotExist) {
								_ = os.RemoveAll(directory)
							}
							return "", serr
						}
						return "snapshot (HTML entity, read-only)", nil
					}
					if _, berr := os.Stat(basePath); errors.Is(berr, os.ErrNotExist) {
						_ = os.RemoveAll(directory)
					}
					return "", err
				}
				return res, nil
			}()

			ident := fmt.Sprintf("%s/%d", entity, eid)
			if workErr != nil {
				counts["✗"]++
				_, _ = fmt.Fprintf(stdout, "✗ %s  %s  %s\n", docPath, ident, workErr.Error())
			} else {
				m := "✓"
				if summary == sync.UpToDate {
					m = "·"
				}
				counts[m]++
				_, _ = fmt.Fprintf(stdout, "%s %s  %s  %s\n", m, docPath, ident, summary)
				taken[dirName] = true
			}
			for _, line := range strings.Split(strings.TrimSuffix(capErr.String(), "\n"), "\n") {
				if line != "" {
					_, _ = fmt.Fprintf(stderr, "    %s\n", line)
				}
			}
			if ctx.Err() != nil {
				interrupted = true
				break
			}
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
	if len(tally) > 0 {
		_, _ = fmt.Fprintln(stdout, strings.Join(tally, ", "))
	}
	if interrupted || counts["✗"] > 0 {
		return ExitError
	}
	return ExitOK
}

func runClone(ctx context.Context, cmd *Command, stdout, stderr io.Writer, env Env) int {
	if (cmd.ID == "") == (!cmd.HasAll) {
		_, _ = fmt.Fprintln(stderr, "clone needs exactly one of <id> or --all")
		return ExitError
	}
	if cmd.HasAll {
		return runCloneAll(ctx, cmd, stdout, stderr, env)
	}
	if _, err := cloneTo(ctx, cmd.ID, cmd.Entity, cmd.Output, cmd.Profile, nil, nil, "", stdout, stderr, env); err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return ExitError
	}
	return ExitOK
}

func runMerge(ctx context.Context, cmd *Command, stdout, stderr io.Writer, env Env) error {
	data, _, err := docTarget(cmd.Doc, cmd.Profile, cmd.Entity, stderr, env, nil)
	if err != nil {
		return err
	}
	return sync.Merge(ctx, cmd.Doc, data, cmd.Profile, cmd.Resolved, stdout)
}
