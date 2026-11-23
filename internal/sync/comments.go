package sync

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/youthesame/elport/internal/config"
	"github.com/youthesame/elport/internal/frontmatter"
)

func ShowComments(ctx context.Context, api API, entity string, eid int, stdout io.Writer) error {
	thread, err := api.Comments(ctx, entity, eid)
	if err != nil {
		return err
	}
	if len(thread) == 0 {
		_, err := fmt.Fprintln(stdout, "no comments")
		return err
	}
	blocks := make([]string, 0, len(thread))
	for _, item := range thread {
		edited := ""
		if strField(item, "modified_at") != strField(item, "created_at") {
			edited = " (edited)"
		}
		blocks = append(blocks, fmt.Sprintf("%s %s%s\n%s", strField(item, "fullname"), strField(item, "created_at"), edited, strField(item, "comment")))
	}
	_, err = fmt.Fprintln(stdout, strings.Join(blocks, "\n\n"))
	return err
}

func Comments(ctx context.Context, docPath string, api API, cfg *config.Data, stdout io.Writer) error {
	doc, err := ReadDocument(docPath, cfg)
	if err != nil {
		return err
	}
	if !doc.HasID {
		return errors.New("id is required")
	}
	return ShowComments(ctx, api, doc.Entity, doc.ID, stdout)
}

func PostComment(ctx context.Context, api API, entity string, eid int, text string, stdout io.Writer) error {
	if strings.TrimSpace(text) == "" {
		return errors.New("comment text is empty")
	}
	validID, err := frontmatter.ValidateID(eid)
	if err != nil {
		return err
	}
	if err := api.AddComment(ctx, entity, validID, text); err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "commented on %s/%d\n", entity, validID)
	return err
}

func Comment(ctx context.Context, docPath string, api API, cfg *config.Data, text string, stdout io.Writer) error {
	if strings.TrimSpace(text) == "" {
		return errors.New("comment text is empty")
	}
	doc, err := ReadDocument(docPath, cfg)
	if err != nil {
		return err
	}
	if !doc.HasID {
		return errors.New("id is required")
	}
	return PostComment(ctx, api, doc.Entity, doc.ID, text, stdout)
}
