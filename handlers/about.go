package handlers

import (
	"net/http"

	"github.com/0xb0b1/blog/i18n"
	"github.com/0xb0b1/blog/templates"
)

type AboutHandler struct{}

func (h *AboutHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	lang := extractLang(r.URL.Path)
	t := i18n.Get(lang)

	component := templates.Base(t.NavAbout+" - Paulo's Blog", lang, r.URL.Path, templates.About(lang))
	if err := component.Render(r.Context(), w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
