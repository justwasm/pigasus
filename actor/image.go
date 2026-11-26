package actor

// ImagePart is an image attachment carried with a conversation message.
// Data contains the original image bytes, not a base64-encoded string.
type ImagePart struct {
	MIMEType string `json:"mime_type,omitempty"`
	Data     []byte `json:"data,omitempty"`
}

func cloneImageParts(images []ImagePart) []ImagePart {
	if images == nil {
		return nil
	}
	cloned := make([]ImagePart, len(images))
	for i, image := range images {
		cloned[i] = image
		if image.Data != nil {
			cloned[i].Data = append([]byte{}, image.Data...)
		}
	}
	return cloned
}
