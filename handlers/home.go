package handlers

import (
	"log"
	"net/http"
	"strings"

	"github.com/0xb0b1/blog/i18n"
	"github.com/0xb0b1/blog/models"
	"github.com/0xb0b1/blog/storage"
	"github.com/0xb0b1/blog/templates"
)

type HomeHandler struct {
	PostsByLang map[i18n.Lang][]models.Post
	Visits      *storage.VisitCounter
}

func (h *HomeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Extract language from path (e.g., /en/ or /pt/)
	lang := extractLang(r.URL.Path)

	// Only handle exact home paths like /en/ or /pt/
	if r.URL.Path != "/"+string(lang)+"/" {
		http.NotFound(w, r)
		return
	}

	t := i18n.Get(lang)
	visitCount := h.Visits.Increment()
	component := templates.Base(t.NavHome+" - Paulo's Blog", lang, r.URL.Path, visitCount, templates.Home(lang))
	if err := component.Render(r.Context(), w); err != nil {
		log.Printf("Error executing template: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
}

// extractLang extracts the language from a URL path
func extractLang(path string) i18n.Lang {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) > 0 {
		return i18n.GetLang(parts[0])
	}
	return i18n.EN
}
