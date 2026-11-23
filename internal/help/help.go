package help

import (
	"embed"
	"fmt"
	"strings"
)

//go:embed agents.md documents.md workflow.md
var topicsFS embed.FS

type Topic struct {
	Name    string
	Summary string
}

var Topics = []Topic{
	{Name: "agents", Summary: "read this first when you are an agent acting for a user"},
	{Name: "workflow", Summary: "the status, diff, push loop; conflicts; pull and fetch"},
	{Name: "documents", Summary: "front matter, references, attachments, links between documents"},
}

func Summary() string {
	var b strings.Builder
	b.WriteString("help topics:\n")
	for _, topic := range Topics {
		fmt.Fprintf(&b, "  elport help %-10s %s\n", topic.Name, topic.Summary)
	}
	return b.String()
}

func Read(topic string) (string, bool) {
	for _, t := range Topics {
		if t.Name == topic {
			data, err := topicsFS.ReadFile(topic + ".md")
			if err != nil {
				return "", false
			}
			return string(data), true
		}
	}
	return "", false
}
