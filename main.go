package main

import (
	"log"
	"net/http"
	"os"

	"github.com/0xb0b1/blog/handlers"
	"github.com/0xb0b1/blog/i18n"
	"github.com/0xb0b1/blog/models"
	"github.com/0xb0b1/blog/storage"
)

func main() {
	// Load blog posts for all languages
	postsByLang := make(map[i18n.Lang][]models.Post)

	for _, lang := range i18n.SupportedLanguages() {
		posts, err := models.LoadPosts("content/posts", string(lang))
		if err != nil {
			log.Printf("Warning: Failed to load posts for %s: %v", lang, err)
			postsByLang[lang] = []models.Post{}
		} else {
			postsByLang[lang] = posts
			log.Printf("Loaded %d posts for %s", len(posts), lang)
		}
	}

	// Initialize visit counter
	visits, err := storage.NewVisitCounter("data/visits.json")
	if err != nil {
		log.Fatalf("Failed to initialize visit counter: %v", err)
	}
	log.Printf("Visit counter initialized with %d visits", visits.Get())

	// Setup routes
	mux := http.NewServeMux()

	// Handlers with language support
	homeHandler := &handlers.HomeHandler{
		PostsByLang: postsByLang,
		Visits:      visits,
	}

	postsHandler := &handlers.PostsHandler{
		PostsByLang: postsByLang,
		Visits:      visits,
	}

	aboutHandler := &handlers.AboutHandler{
		Visits: visits,
	}

	// Root redirect to default language
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/en/", http.StatusFound)
			return
		}
		http.NotFound(w, r)
	})

	// Language-prefixed routes
	mux.Handle("/en/", homeHandler)
	mux.Handle("/pt/", homeHandler)
	mux.Handle("/en/posts/", postsHandler)
	mux.Handle("/pt/posts/", postsHandler)
	mux.Handle("/en/about", aboutHandler)
	mux.Handle("/pt/about", aboutHandler)

	// Static files
	fs := http.FileServer(http.Dir("static"))
	mux.Handle("/static/", http.StripPrefix("/static/", fs))

	// Get port from environment or use default
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Start server
	log.Printf("Server starting on http://localhost:%s", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
