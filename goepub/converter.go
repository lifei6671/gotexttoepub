package goepub

import "context"

// Converter converts a book to another format
type Converter interface {
	Convert(ctx context.Context, book *Book) error
}
