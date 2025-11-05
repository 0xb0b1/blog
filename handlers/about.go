package handlers

import (
	"net/http"

	"github.com/0xb0b1/blog/templates"
)

type AboutHandler struct{}

func (h *AboutHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	component := templates.Base("About - Paulo's Blog", templates.About())
	if err := component.Render(r.Context(), w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
