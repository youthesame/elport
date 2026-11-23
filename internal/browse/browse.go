package browse

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultLimit = 50
	SearchPage   = 100
	Weeks        = 53
)

var Scopes = map[string]int{
	"self": 1,
	"team": 2,
	"all":  3,
}

var OrderColumns = map[string]string{
	"lastchange": "modified_at",
	"comment":    "recent_comment",
}

var (
	glyphs  = []rune("·░▒▓█")
	palette = []int{240, 22, 28, 34, 40}
	months  = []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}
	labels  = []string{"Mon", "", "Wed", "", "Fri", "", "Sun"}
)

type Searcher interface {
	Search(ctx context.Context, entity string, params url.Values) ([]map[string]any, error)
}

type ListOptions struct {
	Entity string
	Scope  string
	Limit  *int
	Offset int
	Query  string
	AsJSON bool
	Order  string
	Sort   string
	Tags   []string
}

func Every(ctx context.Context, client Searcher, entity, scope, query string, offset int, filters url.Values) ([]map[string]any, error) {
	scopeID, ok := Scopes[scope]
	if !ok {
		return nil, errors.New("scope must be one of: self, team, all")
	}
	var all []map[string]any
	cur := offset
	for {
		params := url.Values{}
		for k, vals := range filters {
			for _, v := range vals {
				params.Add(k, v)
			}
		}
		params.Set("scope", strconv.Itoa(scopeID))
		params.Set("limit", strconv.Itoa(SearchPage))
		params.Set("offset", strconv.Itoa(cur))
		if query != "" {
			params.Set("q", query)
		}
		rows, err := client.Search(ctx, entity, params)
		if err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			return all, nil
		}
		all = append(all, rows...)
		cur += len(rows)
	}
}

func stringField(row map[string]any, key string) string {
	v, ok := row[key]
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

func Entities(ctx context.Context, client Searcher, opts ListOptions, stdout, stderr io.Writer) error {
	scopeID, ok := Scopes[opts.Scope]
	if !ok {
		return errors.New("scope must be one of: self, team, all")
	}
	if opts.Limit != nil && *opts.Limit < 1 {
		return errors.New("limit must be a positive integer")
	}
	if opts.Offset < 0 {
		return errors.New("offset must not be negative")
	}

	filters := url.Values{}
	if opts.Order != "" {
		filters.Set("order", opts.Order)
	}
	if opts.Sort != "" {
		filters.Set("sort", opts.Sort)
	}
	for _, tag := range opts.Tags {
		filters.Add("tags[]", tag)
	}

	var rows []map[string]any
	var err error
	if opts.Limit == nil {
		rows, err = Every(ctx, client, opts.Entity, opts.Scope, opts.Query, opts.Offset, filters)
	} else {
		params := url.Values{}
		for k, vals := range filters {
			for _, v := range vals {
				params.Add(k, v)
			}
		}
		params.Set("scope", strconv.Itoa(scopeID))
		params.Set("limit", strconv.Itoa(*opts.Limit))
		params.Set("offset", strconv.Itoa(opts.Offset))
		if opts.Query != "" {
			params.Set("q", opts.Query)
		}
		rows, err = client.Search(ctx, opts.Entity, params)
	}
	if err != nil {
		return err
	}

	if opts.AsJSON {
		if rows == nil {
			rows = []map[string]any{}
		}
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rows); err != nil {
			return err
		}
		_, _ = io.WriteString(stdout, buf.String())
	} else if len(rows) == 0 {
		_, _ = fmt.Fprintf(stdout, "no %s found\n", opts.Entity)
	} else {
		col := OrderColumns[opts.Order]
		if col == "" {
			col = "date"
		}
		idWidth := 0
		whenWidth := 0
		for _, row := range rows {
			if n := len(stringField(row, "id")); n > idWidth {
				idWidth = n
			}
			if n := len(stringField(row, col)); n > whenWidth {
				whenWidth = n
			}
		}
		for _, row := range rows {
			idStr := stringField(row, "id")
			whenStr := stringField(row, col)
			title := stringField(row, "title")
			line := fmt.Sprintf("%-*s  %-*s  %s", idWidth, idStr, whenWidth, whenStr, title)
			_, _ = fmt.Fprintln(stdout, strings.TrimRight(line, " \t"))
		}
	}

	if opts.Limit != nil && len(rows) >= *opts.Limit && stderr != nil {
		_, _ = fmt.Fprintf(stderr, "showing %d; raise --limit or page with --offset for more\n", *opts.Limit)
	}
	return nil
}

func mondayIndex(wd time.Weekday) int {
	return (int(wd) + 6) % 7
}

func cell(count int, color bool) string {
	level := min(count, 4)
	g := string(glyphs[level])
	if color {
		return fmt.Sprintf("\033[38;5;%dm%s\033[0m", palette[level], g)
	}
	return g
}

func monthHeader(start time.Time, weeks int) string {
	type mark struct {
		week  int
		month time.Month
	}
	marks := []mark{{week: 0, month: start.Month()}}
	for week := range weeks {
		for wd := range 7 {
			day := start.AddDate(0, 0, week*7+wd)
			if day.Day() == 1 {
				marks = append(marks, mark{week: week, month: day.Month()})
			}
		}
	}
	if len(marks) > 1 && marks[1].week < 2 {
		marks = marks[1:]
	}
	line := make([]byte, 2*weeks+1)
	for i := range line {
		line[i] = ' '
	}
	for _, m := range marks {
		name := months[m.month-1]
		copy(line[2*m.week:], name)
	}
	return strings.TrimRight("    "+string(line), " ")
}

func Activity(rows []map[string]any, entity string, today time.Time, color bool, width int) string {
	todayDay := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
	weeks := (width - 5) / 2
	if weeks < 1 {
		weeks = 1
	}
	if weeks > Weeks {
		weeks = Weeks
	}
	start := todayDay.AddDate(0, 0, -(mondayIndex(todayDay.Weekday()) + (weeks-1)*7))

	type authorKey struct {
		name   string
		userid string
	}
	days := map[authorKey]map[string]int{}
	for _, row := range rows {
		created := stringField(row, "created_at")
		if len(created) < 10 {
			continue
		}
		day, err := time.Parse("2006-01-02", created[:10])
		if err != nil {
			continue
		}
		if day.Before(start) || day.After(todayDay) {
			continue
		}
		uid := stringField(row, "userid")
		name := stringField(row, "fullname")
		if name == "" {
			if uid == "" {
				name = "user None"
			} else {
				name = "user " + uid
			}
		}
		key := authorKey{name: name, userid: uid}
		if days[key] == nil {
			days[key] = map[string]int{}
		}
		days[key][day.Format("2006-01-02")]++
	}

	if len(days) == 0 {
		return fmt.Sprintf("no %s created since %s\n", entity, start.Format("2006-01-02"))
	}

	type authorEntry struct {
		key    authorKey
		counts map[string]int
		total  int
	}
	entries := make([]authorEntry, 0, len(days))
	for k, counts := range days {
		total := 0
		for _, c := range counts {
			total += c
		}
		entries = append(entries, authorEntry{key: k, counts: counts, total: total})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].total != entries[j].total {
			return entries[i].total > entries[j].total
		}
		return entries[i].key.name < entries[j].key.name
	})

	noun := strings.TrimRight(entity, "s")
	lines := []string{fmt.Sprintf("%s created per day · %s → %s", entity, start.Format("2006-01-02"), todayDay.Format("2006-01-02"))}
	for _, entry := range entries {
		total := entry.total
		active := len(entry.counts)
		nounPlural := ""
		if total != 1 {
			nounPlural = "s"
		}
		dayPlural := ""
		if active != 1 {
			dayPlural = "s"
		}
		heading := fmt.Sprintf("%s  %d %s%s · %d active day%s", entry.key.name, total, noun, nounPlural, active, dayPlural)
		lines = append(lines, "", heading, monthHeader(start, weeks))
		for wd, label := range labels {
			cells := make([]string, 0, weeks)
			for week := range weeks {
				day := start.AddDate(0, 0, week*7+wd)
				if day.After(todayDay) {
					cells = append(cells, " ")
				} else {
					cells = append(cells, cell(entry.counts[day.Format("2006-01-02")], color))
				}
			}
			line := fmt.Sprintf("%-4s%s", label, strings.Join(cells, " "))
			lines = append(lines, strings.TrimRight(line, " "))
		}
	}

	legendParts := make([]string, 0, 5)
	for level := range 5 {
		plus := ""
		if level == 4 {
			plus = "+"
		}
		legendParts = append(legendParts, fmt.Sprintf("%s %d%s", cell(level, color), level, plus))
	}
	lines = append(lines, "", "    "+strings.Join(legendParts, "  "))
	return strings.Join(lines, "\n") + "\n"
}
