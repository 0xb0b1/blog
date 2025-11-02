package models

import (
	"bytes"
	"html/template"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	meta "github.com/yuin/goldmark-meta"
	"github.com/yuin/goldmark/parser"
	"github.com/alecthomas/chroma/v2/formatters/html"
)

type Post struct {
	Title       string
	Slug        string
	Date        time.Time
	Description string
	Tags        []string
	Content     template.HTML
	ReadingTime int
}

var markdown = goldmark.New(
	goldmark.WithExtensions(
		meta.Meta,
		highlighting.NewHighlighting(
			highlighting.WithFormatOptions(
				html.WithClasses(true), // Use CSS classes instead of inline styles
				html.WithLineNumbers(false),
			),
		),
	),
)

// LoadPosts reads all markdown files from the content/posts directory
func LoadPosts(contentDir string) ([]Post, error) {
	var posts []Post

	err := filepath.WalkDir(contentDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}

		post, err := parsePost(path)
		if err != nil {
			return err
		}

		posts = append(posts, post)
		return nil
	})

	if err != nil {
		return nil, err
	}

	// Sort posts by date (newest first)
	sort.Slice(posts, func(i, j int) bool {
		return posts[i].Date.After(posts[j].Date)
	})

	return posts, nil
}

func parsePost(path string) (Post, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return Post{}, err
	}

	var buf bytes.Buffer
	context := parser.NewContext()

	if err := markdown.Convert(content, &buf, parser.WithContext(context)); err != nil {
		return Post{}, err
	}

	metaData := meta.Get(context)

	// Extract metadata
	title := getStringMeta(metaData, "title", "Untitled")
	date := getDateMeta(metaData, "date")
	description := getStringMeta(metaData, "description", "")
	tags := getSliceMeta(metaData, "tags")

	// Generate slug from filename
	filename := filepath.Base(path)
	slug := strings.TrimSuffix(filename, ".md")

	// Calculate reading time (average 200 words per minute)
	wordCount := len(strings.Fields(string(content)))
	readingTime := int(math.Ceil(float64(wordCount) / 200.0))

	return Post{
		Title:       title,
		Slug:        slug,
		Date:        date,
		Description: description,
		Tags:        tags,
		Content:     template.HTML(buf.String()),
		ReadingTime: readingTime,
	}, nil
}

func getStringMeta(data map[string]interface{}, key, defaultVal string) string {
	if val, ok := data[key]; ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return defaultVal
}

func getDateMeta(data map[string]interface{}, key string) time.Time {
	if val, ok := data[key]; ok {
		if str, ok := val.(string); ok {
			// Try parsing common date formats
			formats := []string{
				"2006-01-02",
				"2006-01-02 15:04:05",
				time.RFC3339,
			}
			for _, format := range formats {
				if t, err := time.Parse(format, str); err == nil {
					return t
				}
			}
		}
	}
	return time.Now()
}

func getSliceMeta(data map[string]interface{}, key string) []string {
	if val, ok := data[key]; ok {
		if slice, ok := val.([]interface{}); ok {
			result := make([]string, 0, len(slice))
			for _, item := range slice {
				if str, ok := item.(string); ok {
					result = append(result, str)
				}
			}
			return result
		}
	}
	return []string{}
}
