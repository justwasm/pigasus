package codec

import (
	"encoding/base64"
	"encoding/json"
	"strings"

	"github.com/justwasm/pigasus/geminicode"
)

// ParseDataURI decodes a data URI. Invalid or unsupported values return ok=false.
func ParseDataURI(uri string) (mime string, data []byte, ok bool) {
	if !strings.HasPrefix(uri, "data:") {
		return "", nil, false
	}
	comma := strings.Index(uri, ",")
	if comma < 0 {
		return "", nil, false
	}
	meta := uri[5:comma]
	dataStr := uri[comma+1:]
	isBase64 := strings.Contains(meta, ";base64")
	mime = strings.TrimSpace(strings.Split(meta, ";")[0])
	if mime == "" {
		mime = "application/octet-stream"
	}
	if isBase64 {
		decoded, err := base64.StdEncoding.DecodeString(dataStr)
		if err != nil {
			return "", nil, false
		}
		return mime, decoded, true
	}
	return mime, []byte(dataStr), true
}

// ParseOpenAIContent maps Chat Completions text and media parts to a turn.
// References that fail to decode or resolve are ignored, matching proxy behavior.
func ParseOpenAIContent(raw any, media Media) (string, []geminicode.TurnImage) {
	addReference := func(images *[]geminicode.TurnImage, uri string) {
		if mime, data, ok := ParseDataURI(uri); ok {
			*images = append(*images, geminicode.TurnImage{MIMEType: mime, Data: data})
		} else if media.Resolve != nil {
			if mime, data, err := media.Resolve(uri); err == nil {
				*images = append(*images, geminicode.TurnImage{MIMEType: mime, Data: data})
			}
		}
	}
	switch v := raw.(type) {
	case string:
		return v, nil
	case []any:
		var textParts []string
		var images []geminicode.TurnImage
		for _, item := range v {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			t, _ := m["type"].(string)
			switch t {
			case "text", "input_text", "output_text":
				if txt, ok := m["text"].(string); ok {
					textParts = append(textParts, txt)
				}
			case "image_url", "input_image":
				if iu, ok := m["image_url"].(map[string]any); ok {
					if urlStr, ok := iu["url"].(string); ok {
						addReference(&images, urlStr)
					}
				}
			case "image":
				if imgStr, ok := m["image"].(string); ok {
					if mime, data, ok := ParseDataURI(imgStr); ok {
						images = append(images, geminicode.TurnImage{MIMEType: mime, Data: data})
					} else if decoded, err := base64.StdEncoding.DecodeString(imgStr); err == nil {
						images = append(images, geminicode.TurnImage{MIMEType: media.detectMIME(decoded), Data: decoded})
					} else if media.Resolve != nil {
						if mime, data, err := media.Resolve(imgStr); err == nil {
							images = append(images, geminicode.TurnImage{MIMEType: mime, Data: data})
						}
					}
				} else if iu, ok := m["image_url"].(map[string]any); ok {
					if urlStr, ok := iu["url"].(string); ok {
						addReference(&images, urlStr)
					}
				}
			case "file", "document":
				var dataURI, mime string
				if rawData, ok := m["data"].(string); ok {
					dataURI = rawData
				}
				if rawMime, ok := m["mimeType"].(string); ok {
					mime = rawMime
				} else if rawMime, ok := m["media_type"].(string); ok {
					mime = rawMime
				}
				if fileObj, ok := m["file"].(map[string]any); ok {
					if u, ok := fileObj["url"].(string); ok {
						dataURI = u
					}
					if mStr, ok := fileObj["mimeType"].(string); ok {
						mime = mStr
					}
				}
				if dataURI != "" {
					if dMime, data, ok := ParseDataURI(dataURI); ok {
						if mime == "" {
							mime = dMime
						}
						images = append(images, geminicode.TurnImage{MIMEType: mime, Data: data})
					} else if decoded, err := base64.StdEncoding.DecodeString(dataURI); err == nil {
						if mime == "" {
							mime = media.detectMIME(decoded)
						}
						images = append(images, geminicode.TurnImage{MIMEType: mime, Data: decoded})
					} else if media.Resolve != nil {
						if resolvedMIME, data, err := media.Resolve(dataURI); err == nil {
							if mime == "" {
								mime = resolvedMIME
							}
							images = append(images, geminicode.TurnImage{MIMEType: mime, Data: data})
						}
					}
				}
			case "input_audio":
				if aud, ok := m["input_audio"].(map[string]any); ok {
					dataB64, _ := aud["data"].(string)
					format, _ := aud["format"].(string)
					if format == "" {
						format = "wav"
					}
					if decoded, err := base64.StdEncoding.DecodeString(dataB64); err == nil {
						images = append(images, geminicode.TurnImage{MIMEType: "audio/" + format, Data: decoded})
					}
				}
			}
		}
		return strings.Join(textParts, "\n"), images
	default:
		return "", nil
	}
}

// ParseAnthropicSystem joins text blocks in an Anthropic system prompt.
func ParseAnthropicSystem(raw any) string {
	switch v := raw.(type) {
	case string:
		return v
	case []any:
		var parts []string
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				if txt, ok := m["text"].(string); ok {
					parts = append(parts, txt)
				}
			}
		}
		return strings.Join(parts, "\n\n")
	default:
		return ""
	}
}

// ParseAnthropicMessages maps Anthropic message blocks to CloudCode turns.
func ParseAnthropicMessages(msgs []AnthropicMessage, media Media) []geminicode.TurnMessage {
	toolCallNames := make(map[string]string)
	var out []geminicode.TurnMessage
	for _, m := range msgs {
		role := strings.ToLower(strings.TrimSpace(m.Role))
		switch v := m.Content.(type) {
		case string:
			out = append(out, geminicode.TurnMessage{Role: role, Content: v})
		case []any:
			var regularParts []string
			var images []geminicode.TurnImage
			for _, blk := range v {
				block, ok := blk.(map[string]any)
				if !ok {
					continue
				}
				bType, _ := block["type"].(string)
				switch bType {
				case "text":
					txt, _ := block["text"].(string)
					regularParts = append(regularParts, txt)
				case "image", "document":
					if src, ok := block["source"].(map[string]any); ok {
						srcType, _ := src["type"].(string)
						mediaType, _ := src["media_type"].(string)
						if mediaType == "" {
							if bType == "document" {
								mediaType = "application/pdf"
							} else {
								mediaType = "image/png"
							}
						}
						if srcType == "base64" {
							dataB64, _ := src["data"].(string)
							if data, err := base64.StdEncoding.DecodeString(dataB64); err == nil {
								images = append(images, geminicode.TurnImage{MIMEType: mediaType, Data: data})
							}
						} else if srcType == "url" || srcType == "file" || srcType == "path" {
							urlStr, _ := src["url"].(string)
							if urlStr == "" {
								urlStr, _ = src["path"].(string)
							}
							if media.Resolve != nil {
								if mime, data, err := media.Resolve(urlStr); err == nil {
									images = append(images, geminicode.TurnImage{MIMEType: mime, Data: data})
								}
							}
						}
					}
				case "tool_use":
					id, _ := block["id"].(string)
					name, _ := block["name"].(string)
					if id != "" && name != "" {
						toolCallNames[id] = name
					}
					inputRaw, _ := json.Marshal(block["input"])
					out = append(out, geminicode.TurnMessage{Role: "assistant", ToolCalls: []geminicode.TurnToolCall{{ID: id, Name: name, Arguments: inputRaw}}})
				case "tool_result":
					toolUseID, _ := block["tool_use_id"].(string)
					isError, _ := block["is_error"].(bool)
					contentStr := ""
					switch c := block["content"].(type) {
					case string:
						contentStr = c
					default:
						b, _ := json.Marshal(c)
						contentStr = string(b)
					}
					out = append(out, geminicode.TurnMessage{Role: "tool", Name: toolCallNames[toolUseID], ToolCallID: toolUseID, Content: contentStr, IsError: isError})
				}
			}
			if len(regularParts) > 0 || len(images) > 0 {
				out = append(out, geminicode.TurnMessage{Role: role, Content: strings.Join(regularParts, "\n"), Images: images})
			}
		}
	}
	return out
}
