package handlers

import (
	"html/template"
	"net/http"
	"sort"
	"strings"

	"github.com/0xb0b1/blog/models"
)

// PostsHandler handles the posts page

type PostsHandler struct {
	PostsTemplate *template.Template
	PostTemplate  *template.Template
	Posts         []models.Post
}

func (h *PostsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	// Handle single post view
	if strings.HasPrefix(path, "/posts/") && path != "/posts/" {
		slug := strings.TrimPrefix(path, "/posts/")
		h.serveSinglePost(w, r, slug)
		return
	}

	// Handle posts list view
	h.servePostsList(w, r)
}

func (h *PostsHandler) serveSinglePost(w http.ResponseWriter, r *http.Request, slug string) {
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

	if err := h.PostTemplate.ExecuteTemplate(w, "post.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (h *PostsHandler) servePostsList(w http.ResponseWriter, r *http.Request) {
	searchQuery := r.URL.Query().Get("q")
	var filteredPosts []models.Post

	if searchQuery != "" {
		for _, post := range h.Posts {
			if strings.Contains(strings.ToLower(post.Title), strings.ToLower(searchQuery)) ||
				strings.Contains(strings.ToLower(post.Description), strings.ToLower(searchQuery)) {
				filteredPosts = append(filteredPosts, post)
			}
		}
	} else {
		filteredPosts = h.Posts
	}

	// Sort posts by date in descending order
	sort.Slice(filteredPosts, func(i, j int) bool {
		return filteredPosts[i].Date.After(filteredPosts[j].Date)
	})

	data := struct {
		Posts       []models.Post
		SearchQuery string
	}{
		Posts:       filteredPosts,
		SearchQuery: searchQuery,
	}

	if err := h.PostsTemplate.Execute(w, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

