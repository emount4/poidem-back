// Package catalog provides public reference data used by the API.
package catalog

// DictionaryItem is an entry in a public reference dictionary.
type Item struct {
	ID   int64
	Name string
	Slug *string
}
