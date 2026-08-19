package textgen

// Line is one colored line of setup/humiliation text — everything except
// the final cliffhanger line, which is always rendered on its own solid
// red plate (see templates/thumbnail.html) and so carries no Color of its
// own.
type Line struct {
	Text  string `json:"text"`
	Color string `json:"color"`
}

// ThumbnailText is prompts/thumbnail.tmpl's expected response shape.
type ThumbnailText struct {
	Lines     []Line `json:"lines"`
	FinalLine string `json:"final_line"`
}
