package pbmcp

import (
	"encoding/json"
	"errors"
	"testing"
)

func callWith(t *testing.T, h func(e *ToolEvent) (any, error)) string {
	t.Helper()
	s := &Server{tools: map[string]Tool{}}
	s.AddTool(Tool{Name: "t", Handler: h})
	res, rpcErr := s.callTool(nil, nil, toolCallParams{Name: "t"})
	if rpcErr != nil {
		t.Fatal(rpcErr.Message)
	}
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestCallToolResultShapes(t *testing.T) {
	cases := []struct {
		name string
		h    func(e *ToolEvent) (any, error)
		want string
	}{
		{"value", func(*ToolEvent) (any, error) { return map[string]any{"a": 1}, nil },
			`{"content":[{"text":"{\n  \"a\": 1\n}","type":"text"}]}`},
		{"string", func(*ToolEvent) (any, error) { return "hi", nil },
			`{"content":[{"text":"hi","type":"text"}]}`},
		{"error", func(*ToolEvent) (any, error) { return nil, errors.New("boom") },
			`{"content":[{"text":"boom","type":"text"}],"isError":true}`},
		{"result", func(*ToolEvent) (any, error) {
			return Result{Content: []Content{Text("caption"), Image([]byte{1, 2, 3}, "image/png")}}, nil
		}, `{"content":[{"type":"text","text":"caption"},{"type":"image","data":"AQID","mimeType":"image/png"}]}`},
		{"result pointer error", func(*ToolEvent) (any, error) {
			return &Result{Content: []Content{Text("nope")}, IsError: true}, nil
		}, `{"content":[{"type":"text","text":"nope"}],"isError":true}`},
		{"empty result", func(*ToolEvent) (any, error) { return Result{}, nil },
			`{"content":[]}`},
	}
	for _, c := range cases {
		if got := callWith(t, c.h); got != c.want {
			t.Errorf("%s:\n got %s\nwant %s", c.name, got, c.want)
		}
	}
}
