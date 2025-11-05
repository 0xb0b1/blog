package handlers

import (
	"log"
	"net/http"

	"github.com/0xb0b1/blog/models"
	"github.com/0xb0b1/blog/templates"
)

type HomeHandler struct {
	Posts []models.Post
}

func (h *HomeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	component := templates.Base("Home - Paulo's Blog", templates.Home())
	if err := component.Render(r.Context(), w); err != nil {
		log.Printf("Error executing template: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
}
