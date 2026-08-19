// Package render turns generated thumbnail text plus a portrait image into
// a 1280x720 PNG — programmatically, via an HTML/CSS template and a
// headless Chrome screenshot (chromedp), never via an image-generation
// model. Models reliably mangle rendered text; a browser laying out real
// CSS never does.
package render

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"html/template"
	"net/http"
)

// LineView is one colored line, ready to drop into templates/thumbnail.html.
type LineView struct {
	Text  string
	Color string
}

// ViewData is templates/thumbnail.html's template data.
type ViewData struct {
	Lines     []LineView
	FinalLine string
	// PortraitDataURI is template.URL, not string: html/template's default
	// URL sanitizer replaces any "data:" scheme with "#ZgotmplZ" unless the
	// value is explicitly marked safe via this type. Safe here because
	// EncodePortrait builds it from a PNG we read off disk ourselves, never
	// from LLM output.
	PortraitDataURI template.URL
	// BackgroundDataURI is the full-frame blurred backdrop (a still frame
	// from the video's own background clip). Empty falls the template back
	// to a flat dark panel. Same template.URL safety note as PortraitDataURI.
	BackgroundDataURI template.URL
	BadgeEnabled      bool
	BadgeText         string
}

// EncodePortrait turns a PNG's raw bytes into the data: URI
// PortraitDataURI expects. Embedding the image inline (rather than a
// file:// path alongside the HTML) means the rendered page has exactly one
// external dependency — none — which matters once the HTML is written to a
// throwaway temp file for chromedp to navigate to.
func EncodePortrait(png []byte) template.URL {
	return template.URL("data:image/png;base64," + base64.StdEncoding.EncodeToString(png))
}

// EncodeImage is EncodePortrait for an image of unknown type — the blurred
// background frame comes off disk as a PNG or a JPEG, so its MIME type is
// sniffed from the bytes rather than assumed. Same inline-data rationale as
// EncodePortrait. Returns "" for empty input so callers can pass an absent
// background straight through to an empty BackgroundDataURI.
func EncodeImage(data []byte) template.URL {
	if len(data) == 0 {
		return ""
	}
	mime := http.DetectContentType(data)
	return template.URL("data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data))
}

// BuildHTML executes tmpl (templates/thumbnail.html, already parsed)
// against data. Uses html/template, not text/template — the lines and
// badge text ultimately come from an LLM response, and html/template's
// contextual escaping is what keeps that from ever being interpreted as
// markup.
func BuildHTML(tmpl *template.Template, data ViewData) (string, error) {
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("render: build html: %w", err)
	}
	return buf.String(), nil
}
