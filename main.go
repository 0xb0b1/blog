package main

import (
	"html/template"
	"log"
	"net/http"
	"os"

	"github.com/0xb0b1/blog/handlers"
	"github.com/0xb0b1/blog/models"
)

func main() {
	// Load blog posts
	posts, err := models.LoadPosts("content/posts")
	if err != nil {
		log.Printf("Warning: Failed to load posts: %v", err)
		posts = []models.Post{} // Continue with empty posts
	}

	log.Printf("Loaded %d posts", len(posts))

	// Parse templates separately for each handler to avoid block conflicts
	homeTmpl, err := template.ParseFiles("templates/base.html", "templates/home.html")
	if err != nil {
		log.Fatalf("Failed to parse home templates: %v", err)
	}

	postTmpl, err := template.ParseFiles("templates/base.html", "templates/post.html")
	if err != nil {
		log.Fatalf("Failed to parse post templates: %v", err)
	}

	aboutTmpl, err := template.ParseFiles("templates/base.html", "templates/about.html")
	if err != nil {
		log.Fatalf("Failed to parse about templates: %v", err)
	}

	// Setup routes
	mux := http.NewServeMux()

	// Handlers
	homeHandler := &handlers.HomeHandler{
		Posts:    posts,
		Template: homeTmpl,
	}

	postHandler := &handlers.PostHandler{
		Posts:    posts,
		Template: postTmpl,
	}

	aboutHandler := &handlers.AboutHandler{
		Template: aboutTmpl,
	}

	// Routes
	mux.Handle("/", homeHandler)
	mux.Handle("/posts/", postHandler)
	mux.Handle("/about", aboutHandler)

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
