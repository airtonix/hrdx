// The reference peer uses only the standard library, not hrdx internals.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"slices"
)

func main() {
	encoder := json.NewEncoder(os.Stdout)
	emit := func(frame any) {
		if encoder.Encode(frame) != nil {
			os.Exit(1)
		}
	}
	emit(map[string]any{"type": "hello", "plugin": "example.hello", "protocol": 1})
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), 256*1024+1)
	var grants []string
	var sequence int
	var keys int
	greeting := "Hello"
	panel := func(focus bool) {
		sequence++
		emit(map[string]any{
			"type": "request", "id": fmt.Sprint(sequence), "method": "ui.view.open",
			"params": map[string]any{"id": "example.hello.panel", "title": "Example plugin", "lines": []string{"Hello from an external process.", fmt.Sprintf("Content key events: %d", keys), "Escape closes this view."}, "width_pct": 60, "height_pct": 50, "focus": focus},
		})
	}
	for scanner.Scan() {
		var frame struct {
			Type   string          `json:"type"`
			ID     string          `json:"id"`
			Method string          `json:"method"`
			Grants []string        `json:"grants"`
			Config map[string]any  `json:"config"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(scanner.Bytes(), &frame) != nil {
			return
		}
		switch frame.Type {
		case "hello_ack":
			grants = frame.Grants
			if value, ok := frame.Config["greeting"].(string); ok && value != "" {
				greeting = value
			}
			emit(map[string]any{"type": "ready"})
		case "request":
			switch frame.Method {
			case "command.invoke":
				emit(map[string]any{"type": "response", "id": frame.ID, "result": map[string]string{"notification": greeting + " from the example plugin"}})
				if slices.Contains(grants, "ui.view.contribute") {
					panel(true)
				}
			case "provider.query":
				var query struct {
					Query string `json:"query"`
				}
				_ = json.Unmarshal(frame.Params, &query)
				emit(map[string]any{"type": "response", "id": frame.ID, "result": map[string]any{"rows": []map[string]any{
					{"label": greeting + ", you searched for " + query.Query, "id": query.Query},
				}}})
			case "provider.select":
				var row struct {
					ID string `json:"id"`
				}
				_ = json.Unmarshal(frame.Params, &row)
				emit(map[string]any{"type": "response", "id": frame.ID, "result": map[string]string{"notification": "Selected " + row.ID}})
			default:
				emit(map[string]any{"type": "response", "id": frame.ID, "error": map[string]string{"code": "unknown_method", "message": "unsupported method"}})
			}
		case "event":
			if frame.Method == "view.input" {
				var input struct {
					Key string `json:"key"`
				}
				if json.Unmarshal(frame.Params, &input) == nil && input.Key != "" {
					keys++
					panel(false)
				}
			}
		case "shutdown":
			emit(map[string]any{"type": "shutdown_ack"})
			return
		}
	}
}
