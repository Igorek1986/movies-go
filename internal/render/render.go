// Package render provides Go html/template rendering for server-side pages.
package render

import (
	"embed"
	"fmt"
	"html/template"
	"lampa-api/db/models"
	"log"
	"net/http"
	"strings"
)

//go:embed all:templates
var templFS embed.FS

// PageData is the top-level context passed to every template.
type PageData struct {
	User      *models.User
	Path      string    // r.URL.Path — used for active-link highlighting
	Analytics Analytics
	Title     string
	Data      any // page-specific payload
}

type Analytics struct {
	YandexMetrikaID   int64
	GoogleAnalyticsID string
}

// partials are parsed into every page template set.
var partials = []string{
	"templates/base.html",
	"templates/_admin_links.html",
	"templates/macros.html",
}

var funcMap = template.FuncMap{
	"hasPrefix": strings.HasPrefix,
	// plural picks the correct Russian plural form for n.
	// f1 = 1 (книга), f2 = 2-4 (книги), f5 = 5+ (книг)
	"plural": func(n int, f1, f2, f5 string) string {
		n = n % 100
		if n >= 11 && n <= 19 {
			return f5
		}
		switch n % 10 {
		case 1:
			return f1
		case 2, 3, 4:
			return f2
		}
		return f5
	},
	// dict builds map[string]any from alternating key/value pairs — used for template calls.
	"dict": func(pairs ...any) (map[string]any, error) {
		if len(pairs)%2 != 0 {
			return nil, fmt.Errorf("dict: odd number of arguments")
		}
		m := make(map[string]any, len(pairs)/2)
		for i := 0; i < len(pairs); i += 2 {
			k, ok := pairs[i].(string)
			if !ok {
				return nil, fmt.Errorf("dict: key %v is not a string", pairs[i])
			}
			m[k] = pairs[i+1]
		}
		return m, nil
	},
}

var cache = map[string]*template.Template{}

func init() {
	var pages []string
	entries, err := templFS.ReadDir("templates")
	if err != nil {
		log.Fatalf("render: read templates: %v", err)
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, "_") || name == "base.html" || name == "macros.html" {
			continue // partials — not standalone pages
		}
		pages = append(pages, strings.TrimSuffix(name, ".html"))
	}

	for _, page := range pages {
		files := append(append([]string{}, partials...), "templates/"+page+".html")
		t, err := template.New("").Funcs(funcMap).ParseFS(templFS, files...)
		if err != nil {
			log.Printf("render: parse %s: %v", page, err)
			continue
		}
		cache[page] = t
	}
	log.Printf("render: loaded %d page templates", len(cache))
}

// Page renders the named page inside the base layout.
func Page(w http.ResponseWriter, r *http.Request, page string, user *models.User, data any) {
	t, ok := cache[page]
	if !ok {
		http.Error(w, "template not found: "+page, http.StatusInternalServerError)
		return
	}
	pd := PageData{
		User: user,
		Path: r.URL.Path,
		Data: data,
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, "base", pd); err != nil {
		log.Printf("render: %s: %v", page, err)
	}
}
