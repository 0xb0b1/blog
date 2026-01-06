package handlers

import (
	"net/http"
	"sort"
	"strings"

	"github.com/0xb0b1/blog/i18n"
	"github.com/0xb0b1/blog/models"
	"github.com/0xb0b1/blog/storage"
	"github.com/0xb0b1/blog/templates"
)

// PostsHandler handles the posts page
type PostsHandler struct {
	PostsByLang map[i18n.Lang][]models.Post
	Visits      *storage.VisitCounter
}

func (h *PostsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	lang := extractLang(path)
	langPrefix := "/" + string(lang)

	// Handle HTMX search endpoint
	if path == langPrefix+"/posts/search" {
		h.servePostsSearch(w, r, lang)
		return
	}

	// Handle single post view
	if strings.HasPrefix(path, langPrefix+"/posts/") && path != langPrefix+"/posts/" {
		slug := strings.TrimPrefix(path, langPrefix+"/posts/")
		h.serveSinglePost(w, r, slug, lang)
		return
	}

	// Handle posts list view
	h.servePostsList(w, r, lang)
}

func (h *PostsHandler) serveSinglePost(w http.ResponseWriter, r *http.Request, slug string, lang i18n.Lang) {
	posts := h.PostsByLang[lang]
	var post *models.Post
	for i := range posts {
		if posts[i].Slug == slug {
			post = &posts[i]
			break
		}
	}

	if post == nil {
		http.NotFound(w, r)
		return
	}

	visitCount := h.Visits.Increment()
	component := templates.Base(post.Title+" - Paulo's Blog", lang, r.URL.Path, visitCount, templates.Post(*post, lang))
	if err := component.Render(r.Context(), w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (h *PostsHandler) servePostsList(w http.ResponseWriter, r *http.Request, lang i18n.Lang) {
	searchQuery := r.URL.Query().Get("q")
	posts := h.PostsByLang[lang]
	var filteredPosts []models.Post

	if searchQuery != "" {
		for _, post := range posts {
			if strings.Contains(strings.ToLower(post.Title), strings.ToLower(searchQuery)) ||
				strings.Contains(strings.ToLower(post.Description), strings.ToLower(searchQuery)) {
				filteredPosts = append(filteredPosts, post)
			}
		}
	} else {
		filteredPosts = posts
	}

	// Sort posts by date in descending order
	sort.Slice(filteredPosts, func(i, j int) bool {
		return filteredPosts[i].Date.After(filteredPosts[j].Date)
	})

	t := i18n.Get(lang)
	visitCount := h.Visits.Increment()
	component := templates.Base(t.PostsTitle+" - Paulo's Blog", lang, r.URL.Path, visitCount, templates.Posts(filteredPosts, searchQuery, lang))
	if err := component.Render(r.Context(), w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (h *PostsHandler) servePostsSearch(w http.ResponseWriter, r *http.Request, lang i18n.Lang) {
	searchQuery := r.URL.Query().Get("q")
	posts := h.PostsByLang[lang]
	var filteredPosts []models.Post

	if searchQuery != "" {
		for _, post := range posts {
			if strings.Contains(strings.ToLower(post.Title), strings.ToLower(searchQuery)) ||
				strings.Contains(strings.ToLower(post.Description), strings.ToLower(searchQuery)) {
				filteredPosts = append(filteredPosts, post)
			}
		}
	} else {
		filteredPosts = posts
	}

	// Sort posts by date in descending order
	sort.Slice(filteredPosts, func(i, j int) bool {
		return filteredPosts[i].Date.After(filteredPosts[j].Date)
	})

	// Return only the posts list partial for HTMX
	component := templates.PostsList(filteredPosts, searchQuery, lang)
	if err := component.Render(r.Context(), w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
