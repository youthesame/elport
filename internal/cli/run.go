package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/youthesame/elport/internal/browse"
	"github.com/youthesame/elport/internal/client"
	"github.com/youthesame/elport/internal/config"
	"github.com/youthesame/elport/internal/help"
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

func (e Env) termWidth(stdout io.Writer) int {
	if e.TermWidth > 0 {
		return e.TermWidth
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

func readPromptSecret(stdin io.Reader, reader *bufio.Reader, stdout io.Writer) (string, error) {
	_, _ = io.WriteString(stdout, "api_key: ")
	if f, ok := stdin.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		raw, err := term.ReadPassword(int(f.Fd()))
		_, _ = io.WriteString(stdout, "\n")
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(raw)), nil
	}
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func runLogin(cmd *Command, stdin io.Reader, stdout, stderr io.Writer, env Env) error {
	if stdin == nil {
		return errors.New("stdin unavailable for interactive login")
	}
	reader := bufio.NewReader(stdin)
	_, _ = io.WriteString(stdout, "base_url: ")
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	baseURL := strings.TrimSpace(line)
	secret, err := readPromptSecret(stdin, reader, stdout)
	if err != nil {
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
		if _, ok := profs[cmd.ProfileName]; !ok {
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

func resolveClient(data *config.Data, profile string, stderr io.Writer, env Env) (string, *client.Client, error) {
	name, baseURL, apiKey, verifySSL, err := config.Resolve(data, profile, "", env.keyring(), stderr)
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
	_, c, err := resolveClient(data, cmd.Profile, stderr, env)
	if err != nil {
		return "", nil, err
	}
	return entity, c, nil
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
	profileName, c, err := resolveClient(data, cmd.Profile, stderr, env)
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
		runErr = runLogin(cmd, stdin, stdout, stderr, env)
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
	case "clone":
		if cmd.HasAll && cmd.ID != "" {
			runErr = errors.New("clone: pass either an entity id or --all ROOT, not both")
		} else if !cmd.HasAll && cmd.ID == "" {
			runErr = errors.New("clone: pass an entity id or --all ROOT")
		} else {
			runErr = fmt.Errorf("%s: not yet implemented in Go build", cmd.Name)
		}
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
