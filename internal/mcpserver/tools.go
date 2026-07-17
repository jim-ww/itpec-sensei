package mcpserver

import (
	"bytes"
	"fmt"
	"image/png"
	"sync/atomic"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jim-ww/itpec-sensei/internal/core"
)

// toolCtx bundles the dependencies and shared helpers every registered tool
// needs, so each tool group's register function (see tools_question.go,
// tools_answer.go, tools_progress.go) only has to take one argument.
type toolCtx struct {
	c              *core.Core
	sess           *sessionState
	imageViewerCmd string
	imageURLFor    func(q *core.Question, lightMode bool) string
	imageModeFor   func(lightMode bool) string
	embedImageFor  func(q *core.Question) (*mcp.ImageContent, error)
}

// registerTools wires up all MCP tools. baseURL is nil for stdio (no HTTP
// server to serve images from); for --remote it holds the server's current
// public base URL (ngrok URL once the tunnel is up, else the local address),
// used to build an imageUrl alongside the embedded image content.
func registerTools(server *mcp.Server, c *core.Core, sess *sessionState, baseURL *atomic.Pointer[string], imageViewerCmd string) {
	t := &toolCtx{c: c, sess: sess, imageViewerCmd: imageViewerCmd}

	t.imageURLFor = func(q *core.Question, lightMode bool) string {
		if baseURL == nil {
			return ""
		}
		bu := baseURL.Load()
		if bu == nil || *bu == "" {
			return ""
		}
		url := *bu + "/images/" + q.ImageRelPath()
		if lightMode {
			url += "?light=1"
		}
		return url
	}
	t.imageModeFor = func(lightMode bool) string {
		if lightMode {
			return "light"
		}
		return "dark"
	}
	// embedImageFor returns the question image embedded as base64 content,
	// always in original (light) colors regardless of the dark-mode default
	// used for imageUrl/imageMode — this is a fallback for MCP clients that
	// can't fetch imageUrl, so it needs to be readable on its own without
	// requiring the caller to also know to pass lightMode.
	t.embedImageFor = func(q *core.Question) (*mcp.ImageContent, error) {
		img, err := c.Bank.Image(q)
		if err != nil {
			return nil, fmt.Errorf("load image: %w", err)
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			return nil, fmt.Errorf("encode image: %w", err)
		}
		return &mcp.ImageContent{Data: buf.Bytes(), MIMEType: "image/png"}, nil
	}

	t.registerQuestionTools(server)
	t.registerAnswerTools(server)
	t.registerProgressTools(server)
}
