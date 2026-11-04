package core

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Images you add to a message (pasted or dropped in a chat). crew keeps them in
// <home>/images/<chat id>/, gives the agent that folder (--add-dir), and lists their paths at the end
// of the message as "[image: <path>]" lines, so Claude reads each one with its Read tool.

// MaxImage is the largest image crew takes (Claude's Read tool shrinks big ones anyway).
const MaxImage = 20 << 20

var imageExt = map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/gif": ".gif", "image/webp": ".webp"}

// imageDir is where a chat's images are.
func (c *Core) imageDir(id string) string { return filepath.Join(c.cfg.Home, "images", id) }

// SaveImage keeps an image for a chat's next message and returns its path.
func (c *Core) SaveImage(id string, data []byte) (string, error) {
	if _, err := c.task(id); err != nil {
		return "", err
	}
	ext, ok := imageExt[http.DetectContentType(data)]
	switch {
	case len(data) > MaxImage:
		return "", fmt.Errorf("an image is at most %d MB", MaxImage>>20)
	case !ok:
		return "", errors.New("only PNG, JPEG, GIF and WebP images")
	}
	dir := c.imageDir(id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, time.Now().Format("20060102-150405.000000")+ext)
	return path, os.WriteFile(path, data, 0o644)
}

// ImagePath is the path of one of a chat's images, by its file name ("" when there's no such image).
func (c *Core) ImagePath(id, name string) string {
	p := filepath.Join(c.imageDir(id), filepath.Base(name))
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}

// WithImages is the message with its images listed at the end, for the agent to read.
func WithImages(text string, paths []string) string {
	if len(paths) == 0 {
		return text
	}
	var b strings.Builder
	b.WriteString(strings.TrimRight(text, "\n"))
	b.WriteString("\n\nRead these images I attached:")
	for _, p := range paths {
		b.WriteString("\n[image: " + p + "]")
	}
	return b.String()
}

// dirExists reports whether dir is there.
func dirExists(dir string) bool { _, err := os.Stat(dir); return err == nil }
