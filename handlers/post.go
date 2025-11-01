package handlers

import (
	"html/template"
	"net/http"
	"strings"

	"github.com/0xb0b1/blog/models"
)

type PostHandler struct {
	Posts    []models.Post
	Template *template.Template
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

	data := struct {
		Post models.Post
	}{
		Post: *post,
	}

	if err := h.Template.ExecuteTemplate(w, "post.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
