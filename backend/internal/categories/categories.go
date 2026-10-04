// Package categories holds the canonical list of artifact categories. It
// lives in code rather than the database so it is versioned, and it is shown
// to the interface and to agents (over MCP) as a controlled vocabulary.
package categories

type Category struct {
	Slug  string `json:"slug"`  // stored in the database (artifacts.category)
	Label string `json:"label"` // human-friendly label
	Icon  string `json:"icon"`  // emoji for the gallery chip
}

// All is the canonical list, by relevance.
var All = []Category{
	{Slug: "dashboard", Label: "Dashboards & Data", Icon: "📊"},
	{Slug: "report", Label: "Docs & Reports", Icon: "📄"},
	{Slug: "prototype", Label: "Prototypes & UI", Icon: "🧪"},
	{Slug: "productivity", Label: "Productivity", Icon: "🧠"},
	{Slug: "informatics", Label: "Engineering", Icon: "💻"},
	{Slug: "automations", Label: "Automations", Icon: "⚙️"},
	{Slug: "education", Label: "Learning", Icon: "📚"},
	{Slug: "finance", Label: "Finance", Icon: "💸"},
	{Slug: "game", Label: "Games", Icon: "🎮"},
	{Slug: "art", Label: "Art & Design", Icon: "🎨"},
	{Slug: "health", Label: "Health", Icon: "🩺"},
	{Slug: "sport", Label: "Sport", Icon: "🏋️"},
	{Slug: "travel", Label: "Travel", Icon: "✈️"},
	{Slug: "food", Label: "Food & Recipes", Icon: "🍝"},
	{Slug: "entertainment", Label: "Entertainment", Icon: "🎬"},
	{Slug: "relationships", Label: "Relationships", Icon: "💕"},
	{Slug: "utilities", Label: "Utilities", Icon: "🧰"},
	{Slug: "other", Label: "Other", Icon: "✨"},
}

// IsValid reports whether the slug is canonical (or empty).
func IsValid(slug string) bool {
	if slug == "" {
		return true
	}
	for _, c := range All {
		if c.Slug == slug {
			return true
		}
	}
	return false
}

// Slugs lists the canonical slugs, to compose tool descriptions.
func Slugs() []string {
	out := make([]string, 0, len(All))
	for _, c := range All {
		out = append(out, c.Slug)
	}
	return out
}
