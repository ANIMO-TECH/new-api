package codex

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCodexImageGenerationPreservesRequest(t *testing.T) {
	for _, model := range []string{"gpt-image-2", "gpt-image-2.5-sunburst", "gpt-image-2.5-flare"} {
		t.Run(model, func(t *testing.T) {
			request := dto.ImageRequest{Model: model, Prompt: "blue circle", N: common.GetPointer(uint(1)), Quality: "low", Size: "auto"}
			info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeImagesGenerations, ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeCodex, ChannelBaseUrl: "https://chatgpt.com"}}
			converted, err := (&Adaptor{}).ConvertImageRequest(nil, info, request)
			require.NoError(t, err)
			assert.Equal(t, request, converted)
			url, err := (&Adaptor{}).GetRequestURL(info)
			require.NoError(t, err)
			assert.Equal(t, "https://chatgpt.com/backend-api/codex/images/generations", url)
		})
	}
}

func TestCodexImageEditMultipartConvertsSDKFiles(t *testing.T) {
	input := image.NewRGBA(image.Rect(0, 0, 2, 2))
	input.Set(0, 0, color.RGBA{B: 255, A: 255})
	var pngData, jpegData bytes.Buffer
	require.NoError(t, png.Encode(&pngData, input))
	require.NoError(t, jpeg.Encode(&jpegData, input, nil))
	for _, multi := range []bool{false, true} {
		name := "single image"
		if multi {
			name = "multiple images"
		}
		t.Run(name, func(t *testing.T) {
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			for key, value := range map[string]string{"model": "gpt-image-2", "prompt": "edit image", "background": "transparent", "output_compression": "0", "partial_images": "0", "stream": "false"} {
				require.NoError(t, writer.WriteField(key, value))
			}
			field := "image"
			if multi {
				field = "image[]"
			}
			images := [][]byte{jpegData.Bytes()}
			if multi {
				images = append(images, pngData.Bytes())
			}
			for _, data := range images {
				part, err := writer.CreateFormFile(field, "anonymous_file")
				require.NoError(t, err)
				_, err = part.Write(data)
				require.NoError(t, err)
			}
			mask, err := writer.CreateFormFile("mask", "anonymous_file")
			require.NoError(t, err)
			_, err = mask.Write(pngData.Bytes())
			require.NoError(t, err)
			require.NoError(t, writer.Close())
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", &body)
			c.Request.Header.Set("Content-Type", writer.FormDataContentType())
			require.NoError(t, c.Request.ParseMultipartForm(32<<20))
			t.Cleanup(func() { _ = c.Request.MultipartForm.RemoveAll() })
			info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeImagesEdits, ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeCodex, ChannelBaseUrl: "https://chatgpt.com"}}
			converted, err := (&Adaptor{}).ConvertImageRequest(c, info, dto.ImageRequest{Model: "gpt-image-2.5-sunburst", Prompt: "edit image", N: common.GetPointer(uint(1))})
			require.NoError(t, err)
			fields, ok := converted.(map[string]any)
			require.True(t, ok)
			assert.Equal(t, "gpt-image-2.5-sunburst", fields["model"], "preserve mapped model rather than multipart's original model")
			assert.Equal(t, "transparent", fields["background"])
			assert.Equal(t, float64(0), fields["output_compression"])
			assert.Equal(t, float64(0), fields["partial_images"])
			assert.Equal(t, false, fields["stream"])
			assert.NotContains(t, fields, "image")
			refs, ok := fields["images"].([]map[string]string)
			require.True(t, ok)
			require.Len(t, refs, len(images))
			for i, data := range images {
				assert.Equal(t, "data:"+http.DetectContentType(data)+";base64,"+base64.StdEncoding.EncodeToString(data), refs[i]["image_url"])
			}
			assert.Equal(t, map[string]string{"image_url": "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngData.Bytes())}, fields["mask"])
			url, err := (&Adaptor{}).GetRequestURL(info)
			require.NoError(t, err)
			assert.Equal(t, "https://chatgpt.com/backend-api/codex/images/edits", url)
		})
	}
}

func TestCodexImageEditJSONPreservesReferences(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", nil)
	c.Request.Header.Set("Content-Type", "application/json")
	var request dto.ImageRequest
	require.NoError(t, common.Unmarshal([]byte(`{"model":"gpt-image-2","prompt":"edit","images":[{"image_url":"data:image/png;base64,cG5n"},{"file_id":"file-test"}],"background":"opaque","n":1}`), &request))
	converted, err := (&Adaptor{}).ConvertImageRequest(c, &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeImagesEdits}, request)
	require.NoError(t, err)
	body, err := common.Marshal(converted)
	require.NoError(t, err)
	assert.JSONEq(t, `{"model":"gpt-image-2","prompt":"edit","images":[{"image_url":"data:image/png;base64,cG5n"},{"file_id":"file-test"}],"background":"opaque","n":1}`, string(body))
}

func TestCodexImageResponsePreservesImageAndUsage(t *testing.T) {
	for _, mode := range []int{relayconstant.RelayModeImagesGenerations, relayconstant.RelayModeImagesEdits} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
		body := `{"created":123,"data":[{"b64_json":"aW1hZ2U=","generation_id":"gen-test"}],"usage":{"input_tokens":2,"output_tokens":3,"total_tokens":5}}`
		resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(bytes.NewBufferString(body))}
		info := &relaycommon.RelayInfo{RelayMode: mode, ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeCodex}}
		usage, apiErr := (&Adaptor{}).DoResponse(c, resp, info)
		require.Nil(t, apiErr)
		require.IsType(t, &dto.Usage{}, usage)
		assert.Equal(t, 5, usage.(*dto.Usage).TotalTokens)
		assert.JSONEq(t, body, w.Body.String())
	}
}
