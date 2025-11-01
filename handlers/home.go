package handlers

import (
	"html/template"
	"log"
	"net/http"

	"github.com/0xb0b1/blog/models"
)

type HomeHandler struct {
	Posts    []models.Post
	Template *template.Template
}

func (h *HomeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	data := struct {
		Posts []models.Post
	}{
		Posts: h.Posts,
	}

	if err := h.Template.ExecuteTemplate(w, "home.html", data); err != nil {
		log.Printf("Error executing template: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
}
