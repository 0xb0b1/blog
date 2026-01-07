package models

import "slices"

// Pagination holds pagination state and helpers
type Pagination struct {
	CurrentPage int
	TotalPages  int
	TotalItems  int
	PerPage     int
	HasPrev     bool
	HasNext     bool
}

// NewPagination creates pagination from total items and current page
func NewPagination(totalItems, currentPage, perPage int) Pagination {
	if perPage <= 0 {
		perPage = 10
	}
	if currentPage <= 0 {
		currentPage = 1
	}

	totalPages := (totalItems + perPage - 1) / perPage
	if totalPages == 0 {
		totalPages = 1
	}
	if currentPage > totalPages {
		currentPage = totalPages
	}

	return Pagination{
		CurrentPage: currentPage,
		TotalPages:  totalPages,
		TotalItems:  totalItems,
		PerPage:     perPage,
		HasPrev:     currentPage > 1,
		HasNext:     currentPage < totalPages,
	}
}

// Offset returns the starting index for slicing
func (p Pagination) Offset() int {
	return (p.CurrentPage - 1) * p.PerPage
}

// PaginateSlice returns the slice of posts for the current page
func PaginatePosts(posts []Post, page, perPage int) ([]Post, Pagination) {
	pagination := NewPagination(len(posts), page, perPage)

	start := pagination.Offset()
	end := start + pagination.PerPage

	if start >= len(posts) {
		return []Post{}, pagination
	}
	if end > len(posts) {
		end = len(posts)
	}

	return posts[start:end], pagination
}

// CollectTags returns all unique tags from posts with their counts
func CollectTags(posts []Post) map[string]int {
	tags := make(map[string]int)
	for _, post := range posts {
		for _, tag := range post.Tags {
			tags[tag]++
		}
	}
	return tags
}

// FilterByTag returns posts that have the specified tag
func FilterByTag(posts []Post, tag string) []Post {
	if tag == "" {
		return posts
	}

	var filtered []Post
	for _, post := range posts {
		if slices.Contains(post.Tags, tag) {
			filtered = append(filtered, post)
		}
	}
	return filtered
}
