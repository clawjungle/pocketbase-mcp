package pbmcp

import "encoding/base64"

// Result lets a Handler choose the content blocks of its tools/call result,
// for example to return an image the client shows to the model as a picture
// rather than as text. Return a Result or *Result from a Handler; any other
// value is still JSON-encoded into a single text block.
type Result struct {
	Content []Content

	// IsError marks the result as a tool-level failure, the same as a
	// Handler returning an error, but with content of the tool's choosing.
	IsError bool
}

// Content is one MCP content block. Build them with Text and Image.
type Content struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
}

// Text is a text content block.
func Text(s string) Content {
	return Content{Type: "text", Text: s}
}

// Image is an image content block. data is the raw image bytes (they are
// base64-encoded here); mimeType is e.g. "image/png".
func Image(data []byte, mimeType string) Content {
	return Content{Type: "image", Data: base64.StdEncoding.EncodeToString(data), MimeType: mimeType}
}

func (r Result) toMap() map[string]any {
	content := r.Content
	if content == nil {
		content = []Content{}
	}
	out := map[string]any{"content": content}
	if r.IsError {
		out["isError"] = true
	}
	return out
}
