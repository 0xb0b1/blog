# Personal Blog

A minimalist, modern blog built with Go and Markdown.

## Features

- 📝 Write posts in Markdown with YAML frontmatter
- 🎨 Beautiful, minimalist design
- 🌙 Clean typography and responsive layout
- 💻 Syntax highlighting for code blocks
- 🏷️ Tag support
- ⚡ Fast and lightweight (pure Go, no frameworks)
- 📱 Mobile-friendly

## Getting Started

### Prerequisites

- Go 1.25+ installed

### Running the Blog

1. **Run the server:**
   ```bash
   go run main.go
   ```

2. **Visit in your browser:**
   ```
   http://localhost:8080
   ```

### Building for Production

```bash
go build -o blog
./blog
```

To specify a custom port:
```bash
PORT=3000 ./blog
```

## Writing Blog Posts

Create new Markdown files in `content/posts/` with the following format:

```markdown
---
title: "Your Post Title"
date: 2025-01-20
description: "A brief description of your post"
tags: ["golang", "programming", "tutorial"]
---

# Your Post Content

Write your content here using Markdown...
```

### Frontmatter Fields

- `title` (required): The post title
- `date` (required): Publication date in YYYY-MM-DD format
- `description` (optional): Short description shown on the home page
- `tags` (optional): Array of tags

### File Naming

The filename (without .md extension) becomes the post's URL slug:
- `getting-started-with-go.md` → `/posts/getting-started-with-go`

## Project Structure

```
pv-blog/
├── main.go              # Application entry point
├── go.mod               # Go module file
├── handlers/            # HTTP handlers
│   ├── home.go
│   ├── posts.go
│   └── about.go
├── models/              # Data models
│   └── posts.go
├── templates/           # HTML templates
│   ├── base.html
│   ├── home.html
│   ├── post.html
│   └── about.html
├── static/              # Static assets
│   └── css/
│       └── style.css
└── content/             # Blog posts
    └── posts/
        └── *.md
```

## Customization

### Update Your Information

1. **Edit templates/about.html** to update the about page
2. **Edit templates/base.html** to change the site name and navigation
3. **Edit static/css/style.css** to customize colors and styling

### CSS Variables

The design uses CSS custom properties for easy theming. Edit these in `static/css/style.css`:

```css
:root {
    --color-accent: #2563eb;        /* Primary accent color */
    --color-text: #1a1a1a;          /* Main text color */
    --color-text-muted: #6b7280;    /* Muted text color */
    /* ... and more */
}
```

## Technologies Used

- **Go** - Programming language
- **Goldmark** - Markdown parser
- **Chroma** - Syntax highlighting
- **Pure CSS** - No frameworks, custom minimalist design

## License

MIT
