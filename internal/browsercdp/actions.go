package browsercdp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Actions encapsulates high-level browser actions on top of the CDP client.
type Actions struct {
	Client *Client
}

// NewActions returns an Actions wrapper for the given Client.
func NewActions(c *Client) *Actions {
	return &Actions{Client: c}
}

// OpenURL navigates the browser to the specified URL and waits for the load event.
func (a *Actions) OpenURL(ctx context.Context, targetURL string) (string, error) {
	if !strings.HasPrefix(targetURL, "http://") && !strings.HasPrefix(targetURL, "https://") && !strings.HasPrefix(targetURL, "file://") && !strings.HasPrefix(targetURL, "about:") {
		targetURL = "https://" + targetURL
	}

	_, err := a.Client.Call(ctx, "Page.navigate", map[string]any{"url": targetURL})
	if err != nil {
		return "", fmt.Errorf("navigate to %s: %w", targetURL, err)
	}

	// Wait briefly for initial DOM loading
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-time.After(500 * time.Millisecond):
	}

	title, _ := a.Client.Evaluate(ctx, "document.title")
	return fmt.Sprintf("Navigated to %s (Title: %v)", targetURL, title), nil
}

// ReadPage returns the text content of the visible page.
func (a *Actions) ReadPage(ctx context.Context) (string, error) {
	val, err := a.Client.Evaluate(ctx, `
		(function() {
			var body = document.body;
			if (!body) return "";
			return body.innerText || body.textContent || "";
		})()
	`)
	if err != nil {
		return "", fmt.Errorf("read page content: %w", err)
	}
	text, _ := val.(string)
	return strings.TrimSpace(text), nil
}

// GetDOM returns the serialized HTML DOM of the page.
func (a *Actions) GetDOM(ctx context.Context) (string, error) {
	val, err := a.Client.Evaluate(ctx, "document.documentElement.outerHTML")
	if err != nil {
		return "", fmt.Errorf("get dom: %w", err)
	}
	html, _ := val.(string)
	return html, nil
}

// ClickElement clicks a DOM element matching the CSS selector.
func (a *Actions) ClickElement(ctx context.Context, selector string) (string, error) {
	script := fmt.Sprintf(`
		(function() {
			var el = document.querySelector(%q);
			if (!el) throw new Error("element not found: " + %q);
			el.scrollIntoView({behavior: "instant", block: "center"});
			el.click();
			return "clicked " + %q;
		})()
	`, selector, selector, selector)

	res, err := a.Client.Evaluate(ctx, script)
	if err != nil {
		return "", err
	}
	return fmt.Sprint(res), nil
}

// ClickPixel dispatches mouse events to click a specific pixel coordinate.
func (a *Actions) ClickPixel(ctx context.Context, x, y int, button string) (string, error) {
	if button == "" {
		button = "left"
	}

	// mouseMoved -> mousePressed -> mouseReleased
	_, err := a.Client.Call(ctx, "Input.dispatchMouseEvent", map[string]any{
		"type": "mouseMoved",
		"x":    x,
		"y":    y,
	})
	if err != nil {
		return "", err
	}

	_, err = a.Client.Call(ctx, "Input.dispatchMouseEvent", map[string]any{
		"type":       "mousePressed",
		"x":          x,
		"y":          y,
		"button":     button,
		"clickCount": 1,
	})
	if err != nil {
		return "", err
	}

	_, err = a.Client.Call(ctx, "Input.dispatchMouseEvent", map[string]any{
		"type":       "mouseReleased",
		"x":          x,
		"y":          y,
		"button":     button,
		"clickCount": 1,
	})
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("Clicked pixel (%d, %d)", x, y), nil
}

// InputText types text into the currently focused element or specified selector.
func (a *Actions) InputText(ctx context.Context, text, selector string) (string, error) {
	if selector != "" {
		focusScript := fmt.Sprintf(`
			(function() {
				var el = document.querySelector(%q);
				if (!el) throw new Error("element not found: " + %q);
				el.focus();
			})()
		`, selector, selector)
		if _, err := a.Client.Evaluate(ctx, focusScript); err != nil {
			return "", err
		}
	}

	_, err := a.Client.Call(ctx, "Input.insertText", map[string]any{
		"text": text,
	})
	if err != nil {
		return "", fmt.Errorf("input text: %w", err)
	}
	return fmt.Sprintf("Typed %q", text), nil
}

// PressKey sends a key down and key up event for the given key name.
func (a *Actions) PressKey(ctx context.Context, key string) (string, error) {
	_, err := a.Client.Call(ctx, "Input.dispatchKeyEvent", map[string]any{
		"type": "keyDown",
		"key":  key,
	})
	if err != nil {
		return "", err
	}
	_, err = a.Client.Call(ctx, "Input.dispatchKeyEvent", map[string]any{
		"type": "keyUp",
		"key":  key,
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Pressed key %q", key), nil
}

// Scroll scrolls the page in the specified direction.
func (a *Actions) Scroll(ctx context.Context, direction string, amount int) (string, error) {
	if amount <= 0 {
		amount = 300
	}
	deltaY := amount
	if strings.ToLower(direction) == "up" {
		deltaY = -amount
	}

	_, err := a.Client.Call(ctx, "Input.dispatchMouseEvent", map[string]any{
		"type":   "mouseWheel",
		"x":      100,
		"y":      100,
		"deltaX": 0,
		"deltaY": deltaY,
	})
	if err != nil {
		return "", fmt.Errorf("scroll %s: %w", direction, err)
	}
	return fmt.Sprintf("Scrolled %s by %d px", direction, amount), nil
}

// ScrollDOM scrolls a specific DOM element.
func (a *Actions) ScrollDOM(ctx context.Context, selector, direction string, amount int) (string, error) {
	if amount <= 0 {
		amount = 200
	}
	deltaY := amount
	if strings.ToLower(direction) == "up" {
		deltaY = -amount
	}

	script := fmt.Sprintf(`
		(function() {
			var el = document.querySelector(%q);
			if (!el) throw new Error("element not found: " + %q);
			el.scrollTop += %d;
			return "scrolled " + %q + " by " + %d;
		})()
	`, selector, selector, deltaY, selector, deltaY)

	res, err := a.Client.Evaluate(ctx, script)
	if err != nil {
		return "", err
	}
	return fmt.Sprint(res), nil
}

// SelectOption selects a dropdown option by value or text.
func (a *Actions) SelectOption(ctx context.Context, selector, value string) (string, error) {
	script := fmt.Sprintf(`
		(function() {
			var el = document.querySelector(%q);
			if (!el) throw new Error("select element not found: " + %q);
			el.value = %q;
			el.dispatchEvent(new Event('change', {bubbles: true}));
			return "selected " + %q;
		})()
	`, selector, selector, value, value)

	res, err := a.Client.Evaluate(ctx, script)
	if err != nil {
		return "", err
	}
	return fmt.Sprint(res), nil
}

// DragPixelToPixel drags the pointer from (x1, y1) to (x2, y2).
func (a *Actions) DragPixelToPixel(ctx context.Context, x1, y1, x2, y2 int) (string, error) {
	// mousePressed at x1, y1
	_, err := a.Client.Call(ctx, "Input.dispatchMouseEvent", map[string]any{
		"type":       "mousePressed",
		"x":          x1,
		"y":          y1,
		"button":     "left",
		"clickCount": 1,
	})
	if err != nil {
		return "", err
	}

	// mouseMoved to x2, y2
	_, err = a.Client.Call(ctx, "Input.dispatchMouseEvent", map[string]any{
		"type":    "mouseMoved",
		"x":       x2,
		"y":       y2,
		"buttons": 1,
	})
	if err != nil {
		return "", err
	}

	// mouseReleased at x2, y2
	_, err = a.Client.Call(ctx, "Input.dispatchMouseEvent", map[string]any{
		"type":   "mouseReleased",
		"x":      x2,
		"y":      y2,
		"button": "left",
	})
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("Dragged pointer from (%d, %d) to (%d, %d)", x1, y1, x2, y2), nil
}

// ResizeWindow changes the browser viewport size.
func (a *Actions) ResizeWindow(ctx context.Context, width, height int) (string, error) {
	if width <= 0 {
		width = 1280
	}
	if height <= 0 {
		height = 800
	}

	_, err := a.Client.Call(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{
		"width":             width,
		"height":            height,
		"deviceScaleFactor": 1,
		"mobile":            false,
	})
	if err != nil {
		return "", fmt.Errorf("resize window: %w", err)
	}
	return fmt.Sprintf("Resized viewport to %dx%d", width, height), nil
}

// RefreshPage reloads the current browser page.
func (a *Actions) RefreshPage(ctx context.Context) (string, error) {
	_, err := a.Client.Call(ctx, "Page.reload", nil)
	if err != nil {
		return "", fmt.Errorf("reload page: %w", err)
	}
	return "Page refreshed", nil
}

// CaptureScreenshot captures a screenshot of the visible page.
func (a *Actions) CaptureScreenshot(ctx context.Context, workspace, format string) (string, error) {
	if format == "" {
		format = "png"
	}
	format = strings.ToLower(format)
	if format != "jpeg" {
		format = "png"
	}

	res, err := a.Client.Call(ctx, "Page.captureScreenshot", map[string]any{
		"format": format,
	})
	if err != nil {
		return "", fmt.Errorf("capture screenshot: %w", err)
	}

	var snap struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(res, &snap); err != nil {
		return "", err
	}

	rawBytes, err := base64.StdEncoding.DecodeString(snap.Data)
	if err != nil {
		return "", fmt.Errorf("decode screenshot: %w", err)
	}

	// Save to workspace artifacts/screenshots directory
	targetDir := filepath.Join(workspace, ".pigasus", "screenshots")
	if workspace == "" {
		targetDir = filepath.Join(os.TempDir(), "pigasus-screenshots")
	}
	_ = os.MkdirAll(targetDir, 0o755)

	fileName := fmt.Sprintf("screenshot_%d.%s", time.Now().UnixMilli(), format)
	filePath := filepath.Join(targetDir, fileName)
	if err := os.WriteFile(filePath, rawBytes, 0o644); err != nil {
		return "", fmt.Errorf("write screenshot file: %w", err)
	}

	return fmt.Sprintf("Screenshot saved to %s (%d bytes)", filePath, len(rawBytes)), nil
}

// ListPages queries active targets / pages.
func (a *Actions) ListPages(ctx context.Context) (string, error) {
	res, err := a.Client.Call(ctx, "Target.getTargets", nil)
	if err != nil {
		return "", err
	}

	var targets struct {
		TargetInfos []struct {
			TargetID string `json:"targetId"`
			Type     string `json:"type"`
			Title    string `json:"title"`
			URL      string `json:"url"`
		} `json:"targetInfos"`
	}
	if err := json.Unmarshal(res, &targets); err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString("Browser Pages:\n")
	idx := 1
	for _, t := range targets.TargetInfos {
		if t.Type == "page" {
			b.WriteString(fmt.Sprintf("%d. [%s] %s\n   URL: %s\n", idx, t.TargetID, t.Title, t.URL))
			idx++
		}
	}
	if idx == 1 {
		b.WriteString("No active pages found.\n")
	}
	return b.String(), nil
}
