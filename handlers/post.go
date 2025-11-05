package handlers

import (
	"net/http"
	"strings"

	"github.com/0xb0b1/blog/models"
	"github.com/0xb0b1/blog/templates"
)

type PostHandler struct {
	Posts []models.Post
}

func (h *PostHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Extract slug from URL path /posts/slug
	slug := strings.TrimPrefix(r.URL.Path, "/posts/")
	if slug == "" {
		http.NotFound(w, r)
		return
	}

	// Find post by slug
	var post *models.Post
	for i := range h.Posts {
		if h.Posts[i].Slug == slug {
			post = &h.Posts[i]
			break
		}
	}

	if post == nil {
		http.NotFound(w, r)
		return
	}

	component := templates.Base(post.Title+" - Paulo's Blog", templates.Post(*post))
	if err := component.Render(r.Context(), w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
