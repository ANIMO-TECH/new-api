package codex

import (
	"encoding/base64"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
)

func convertImageRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.ImageRequest) (any, error) {
	if info.RelayMode == relayconstant.RelayModeImagesGenerations {
		return request, nil
	}
	if info.RelayMode != relayconstant.RelayModeImagesEdits {
		return nil, fmt.Errorf("codex channel: endpoint not supported")
	}
	body, err := common.Marshal(request)
	if err != nil {
		return nil, err
	}
	var fields map[string]any
	if err := common.Unmarshal(body, &fields); err != nil {
		return nil, err
	}
	if !strings.HasPrefix(c.Request.Header.Get("Content-Type"), "multipart/form-data") {
		return fields, nil
	}
	form := c.Request.MultipartForm
	if form == nil {
		form, err = common.ParseMultipartFormReusable(c)
		if err != nil {
			return nil, err
		}
		c.Request.MultipartForm = form
	}
	// Keep SDK options not represented by the shared multipart request parser.
	for name, values := range form.Value {
		if name == "model" || name == "image" || len(values) == 0 {
			continue
		}
		if _, exists := fields[name]; exists {
			continue
		}
		value := values[0]
		switch name {
		case "output_compression", "partial_images", "stream":
			var parsed any
			if err := common.Unmarshal([]byte(value), &parsed); err != nil {
				return nil, fmt.Errorf("invalid image option %s", name)
			}
			fields[name] = parsed
		default:
			fields[name] = value
		}
	}
	files := append([]*multipart.FileHeader{}, form.File["image"]...)
	files = append(files, form.File["image[]"]...)
	if len(files) == 0 {
		return nil, fmt.Errorf("image is required")
	}
	images := make([]map[string]string, 0, len(files))
	for _, file := range files {
		image, err := imageReferenceFromFile(file)
		if err != nil {
			return nil, err
		}
		images = append(images, image)
	}
	delete(fields, "image")
	fields["images"] = images
	if masks := form.File["mask"]; len(masks) > 0 {
		mask, err := imageReferenceFromFile(masks[0])
		if err != nil {
			return nil, err
		}
		fields["mask"] = mask
	}
	return fields, nil
}

func imageReferenceFromFile(file *multipart.FileHeader) (map[string]string, error) {
	reader, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("failed to open image: %w", err)
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to read image: %w", err)
	}
	contentType := http.DetectContentType(data)
	if !strings.HasPrefix(contentType, "image/") {
		return nil, fmt.Errorf("uploaded file is not an image")
	}
	return map[string]string{"image_url": "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(data)}, nil
}
