package filesystem

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/justwasm/pigasus/imagery"
	"github.com/justwasm/pigasus/internal/app/tools"
)

const maxViewImageBytes = 100 * 1024 * 1024

// readViewFileData rejects oversized images before allocating their contents.
func readViewFileData(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > maxViewImageBytes {
		return nil, fmt.Errorf("image exceeds the 100 MB view limit: %s", path)
	}
	data, err := io.ReadAll(io.LimitReader(file, maxViewImageBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxViewImageBytes {
		return nil, fmt.Errorf("image exceeds the 100 MB view limit: %s", path)
	}
	return data, nil
}

// imageViewResult validates and prepares image bytes before they can enter the
// conversation. The MIME type follows the prepared bytes, not the extension.
func imageViewResult(path string, data []byte, start time.Time) (tools.MediaOutput, error) {
	if err := probeImage(path, data); err != nil {
		return tools.MediaOutput{}, err
	}
	prepared, format, _, err := imagery.Prepare(data)
	if err != nil {
		return tools.MediaOutput{}, fmt.Errorf("invalid image %s: %w", path, err)
	}
	if format == imagery.FormatUnknown {
		return tools.MediaOutput{}, fmt.Errorf("invalid image %s: unsupported or corrupt image data", path)
	}
	return tools.MediaOutput{
		Text: tools.ResultStamp(start) + fmt.Sprintf("File Path: `file://%s`\nTotal Bytes: %d\nImage Type: %s\nThe image is attached to this tool result.", path, len(data), format.MIMEType()),
		Images: []tools.MediaPart{{
			MIMEType: format.MIMEType(),
			Data:     prepared,
		}},
	}, nil
}
