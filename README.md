# Personal Blog

A minimalist, modern blog built with Go, Templ, and HTMX.

## Features

- 📝 Write posts in Markdown with YAML frontmatter
- 🎨 Beautiful, minimalist design with Catppuccin color scheme
- 🌙 Dark/Light mode toggle with localStorage persistence
- 💻 Syntax highlighting for code blocks (Catppuccin theme)
- 🏷️ Tag support
- 🔍 Live search with HTMX (no page reloads)
- ⚡ Fast and lightweight with type-safe templates
- 📱 Mobile-friendly with responsive sidebar navigation
- 🎨 Animated molecular background
- 🔄 Interactive search as you type (300ms debounce)

## Getting Started

### Prerequisites

- Go 1.25+ installed
- Templ CLI (`go install github.com/a-h/templ/cmd/templ@latest`)

### Running the Blog

1. **Generate Templ templates (first time only):**
   ```bash
   templ generate
   ```

2. **Run the server:**
   ```bash
   go run main.go
   ```

3. **Visit in your browser:**
   ```
   http://localhost:8080
   ```

### Building for Production

```bash
templ generate
go build -o blog
./blog
```

To specify a custom port:
```bash
PORT=3000 ./blog
```

### Development Workflow

When editing `.templ` files, run:
```bash
templ generate
```

This regenerates the Go code from your Templ templates. The generated `*_templ.go` files are committed to the repository, so end users don't need Templ installed to build the project.

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
blog/
├── main.go              # Application entry point
├── go.mod               # Go module file
├── handlers/            # HTTP handlers
│   ├── home.go         # Home page handler
│   ├── posts.go        # Posts list, search, and single post handlers
│   ├── post.go         # (deprecated, merged into posts.go)
│   └── about.go        # About page handler
├── models/              # Data models
│   └── post.go         # Post model and markdown parsing
├── templates/           # Templ templates (type-safe)
│   ├── base.templ      # Base layout with sidebar, nav, footer
│   ├── home.templ      # Homepage hero section
│   ├── post.templ      # Single post view
│   ├── posts.templ     # Posts list with search form
│   ├── about.templ     # About page
│   ├── components.templ # Reusable components (PostCard)
│   └── *_templ.go      # Generated Go code (committed to repo)
├── static/              # Static assets
│   ├── css/
│   │   └── style.css   # Catppuccin theme with dark/light modes
│   └── js/
│       └── molecules.js # Animated molecular background
└── content/             # Blog posts
    └── posts/
        └── *.md        # Markdown posts with YAML frontmatter
```

## Customization

### Update Your Information

1. **Edit `templates/about.templ`** to update the about page
2. **Edit `templates/base.templ`** to change the site name and navigation
3. **Edit `templates/home.templ`** to update your hero section and tech stack
4. **Edit `static/css/style.css`** to customize colors and styling

After editing `.templ` files, run `templ generate` to regenerate the Go code.

### Catppuccin Color Theme

The design uses the Catppuccin color scheme with CSS custom properties. The theme automatically switches between Latte (light) and Mocha (dark) variants based on user preference.

#### Light Mode (Latte)
```css
:root {
    --color-bg: #eff1f5;
    --color-text: #4c4f69;
    --color-accent: #1e66f5;
    /* ... */
}
```

#### Dark Mode (Mocha)
```css
[data-theme="dark"] {
    --color-bg: #1e1e2e;
    --color-text: #cdd6f4;
    --color-accent: #89b4fa;
    /* ... */
}
```

Users can toggle between themes using the button in the sidebar. The preference is persisted in localStorage.

## Technologies Used

### Backend
- **Go 1.25+** - Programming language
- **Templ** - Type-safe templating engine (compile-time checking)
- **Goldmark** - Fast, extensible Markdown parser
- **Chroma** - Syntax highlighting with Catppuccin theme
- **Standard library** - No web frameworks, pure `net/http`

### Frontend
- **HTMX 2.0** - Dynamic interactions without writing JavaScript
- **Vanilla JavaScript** - Theme toggle, mobile navigation, molecular canvas
- **Catppuccin** - Beautiful color scheme (Latte/Mocha)
- **Inter + JetBrains Mono** - Modern typography
- **Canvas API** - Animated molecular background

### Architecture
- **Type-safe templates** - Templ provides compile-time validation
- **Progressive enhancement** - Works without JavaScript
- **Partial updates** - HTMX enables SPA-like UX without complexity
- **Component-based** - Reusable Templ components (PostCard, etc.)
- **No template conflicts** - Each Templ component is a Go function

## Key Features Explained

### Live Search with HTMX
The posts search updates results in real-time without page reloads:
- Triggers on form submit or keyup with 300ms debounce
- Only updates the posts list, preserving page state
- Shows loading indicator during search
- Falls back to regular form submission without JavaScript

### Theme System
Dark/Light mode with smooth transitions:
- Catppuccin Mocha (dark) is the default
- Catppuccin Latte (light) is the alternative
- Preference stored in localStorage
- Theme applied before page render to prevent flash
- All syntax highlighting matches the current theme

### Mobile Navigation
Responsive design with sidebar navigation:
- Desktop: Persistent sidebar with navigation
- Mobile: Hamburger menu that slides in from left
- Touch-friendly with proper spacing
- Active link highlighting

## License

MIT
