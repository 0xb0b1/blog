package handlers

import (
	"html/template"
	"net/http"
)

type AboutHandler struct {
	Template *template.Template
}

func (h *AboutHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := h.Template.ExecuteTemplate(w, "about.html", nil); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
