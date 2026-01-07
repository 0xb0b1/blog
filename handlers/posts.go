package handlers

import (
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/0xb0b1/blog/i18n"
	"github.com/0xb0b1/blog/models"
	"github.com/0xb0b1/blog/templates"
)

const postsPerPage = 6

// PostsHandler handles the posts page
type PostsHandler struct {
	PostsByLang map[i18n.Lang][]models.Post
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

	component := templates.Base(post.Title+" - Paulo's Blog", lang, r.URL.Path, templates.Post(*post, lang))
	if err := component.Render(r.Context(), w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (h *PostsHandler) servePostsList(w http.ResponseWriter, r *http.Request, lang i18n.Lang) {
	query := r.URL.Query()
	searchQuery := query.Get("q")
	tagFilter := query.Get("tag")
	page := parsePageParam(query.Get("page"))

	posts := h.PostsByLang[lang]
	allTags := models.CollectTags(posts)

	// Filter posts
	filteredPosts := h.filterPosts(posts, searchQuery, tagFilter)

	// Sort by date descending
	sort.Slice(filteredPosts, func(i, j int) bool {
		return filteredPosts[i].Date.After(filteredPosts[j].Date)
	})

	// Paginate
	paginatedPosts, pagination := models.PaginatePosts(filteredPosts, page, postsPerPage)

	t := i18n.Get(lang)
	component := templates.Base(
		t.PostsTitle+" - Paulo's Blog",
		lang,
		r.URL.Path,
		templates.Posts(paginatedPosts, searchQuery, tagFilter, allTags, pagination, lang),
	)
	if err := component.Render(r.Context(), w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (h *PostsHandler) servePostsSearch(w http.ResponseWriter, r *http.Request, lang i18n.Lang) {
	query := r.URL.Query()
	searchQuery := query.Get("q")
	tagFilter := query.Get("tag")
	page := parsePageParam(query.Get("page"))

	posts := h.PostsByLang[lang]
	allTags := models.CollectTags(posts)

	// Filter posts
	filteredPosts := h.filterPosts(posts, searchQuery, tagFilter)

	// Sort by date descending
	sort.Slice(filteredPosts, func(i, j int) bool {
		return filteredPosts[i].Date.After(filteredPosts[j].Date)
	})

	// Paginate
	paginatedPosts, pagination := models.PaginatePosts(filteredPosts, page, postsPerPage)

	// Return only the posts list partial for HTMX
	component := templates.PostsList(paginatedPosts, searchQuery, tagFilter, allTags, pagination, lang)
	if err := component.Render(r.Context(), w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (h *PostsHandler) filterPosts(posts []models.Post, searchQuery, tagFilter string) []models.Post {
	var filtered []models.Post

	for _, post := range posts {
		// Tag filter
		if tagFilter != "" {
			hasTag := false
			for _, t := range post.Tags {
				if t == tagFilter {
					hasTag = true
					break
				}
			}
			if !hasTag {
				continue
			}
		}

		// Search filter
		if searchQuery != "" {
			searchLower := strings.ToLower(searchQuery)
			if !strings.Contains(strings.ToLower(post.Title), searchLower) &&
				!strings.Contains(strings.ToLower(post.Description), searchLower) {
				continue
			}
		}

		filtered = append(filtered, post)
	}

	return filtered
}

func parsePageParam(s string) int {
	if s == "" {
		return 1
	}
	page, err := strconv.Atoi(s)
	if err != nil || page < 1 {
		return 1
	}
	return page
}
