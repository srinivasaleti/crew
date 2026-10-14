// Package browser opens links in your browser.
package browser

import "os/exec"

// Open opens a URL in the browser; "" does nothing.
func Open(u string) {
	if u != "" {
		if c := exec.Command("open", u); c.Start() == nil {
			go c.Wait()
		}
	}
}
