package sync

import (
	"context"
	"net/url"

	"github.com/youthesame/elport/internal/plan"
)

type API interface {
	Root() string
	Me(ctx context.Context) (map[string]any, error)
	Info(ctx context.Context) (map[string]any, error)
	Get(ctx context.Context, entity string, eid int) (map[string]any, error)
	Create(ctx context.Context, entity, title string) (map[string]any, error)
	Patch(ctx context.Context, entity string, eid int, payload map[string]any) (map[string]any, error)
	Uploads(ctx context.Context, entity string, eid int) ([]map[string]any, error)
	Comments(ctx context.Context, entity string, eid int) ([]map[string]any, error)
	Upload(ctx context.Context, entity string, eid int, approved plan.ApprovedUploadPath) (map[string]any, error)
	Download(ctx context.Context, entity string, eid, uid int) ([]byte, error)
	AddTag(ctx context.Context, entity string, eid int, tag string) error
	AddLink(ctx context.Context, entity string, eid int, targetEntity string, targetID int) error
	AddComment(ctx context.Context, entity string, eid int, text string) error
	Categories(ctx context.Context, teamID any, entity string) ([]map[string]any, error)
	Statuses(ctx context.Context, teamID any, entity string) ([]map[string]any, error)
	Search(ctx context.Context, entity string, params url.Values) ([]map[string]any, error)
}
