package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newCustomTestRequestContext(path string) *gin.Context {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, path, nil)
	c.Request.Header.Set("Content-Type", "application/json")
	return c
}

func TestParseChannelTestRequestValidatesImageBoundsAndForcesModel(t *testing.T) {
	c := newCustomTestRequestContext("/v1/images/generations")
	request, err := parseChannelTestRequest(
		c,
		types.RelayFormatOpenAIImage,
		`{"model":"untrusted","prompt":"blue square","quality":"low","n":1}`,
		"gpt-image-2",
		"image-generation",
		false,
	)
	require.NoError(t, err)
	imageRequest, ok := request.(*dto.ImageRequest)
	require.True(t, ok)
	assert.Equal(t, "gpt-image-2", imageRequest.Model)
	require.NotNil(t, imageRequest.N)
	assert.Equal(t, uint(1), *imageRequest.N)

	c = newCustomTestRequestContext("/v1/images/generations")
	_, err = parseChannelTestRequest(
		c,
		types.RelayFormatOpenAIImage,
		`{"prompt":"blue square","n":129}`,
		"gpt-image-2",
		"image-generation",
		false,
	)
	assert.ErrorContains(t, err, "n must be an integer")
}

func TestParseChannelTestRequestRejectsNonObjectAndOversizedBody(t *testing.T) {
	c := newCustomTestRequestContext("/v1/chat/completions")
	_, err := parseChannelTestRequest(c, types.RelayFormatOpenAI, `[]`, "gpt-4o-mini", "openai", false)
	assert.ErrorContains(t, err, "JSON object")

	c = newCustomTestRequestContext("/v1/chat/completions")
	_, err = parseChannelTestRequest(c, types.RelayFormatOpenAI, `{"value":"`+strings.Repeat("x", maxChannelTestRequestBodyBytes)+`"}`, "gpt-4o-mini", "openai", false)
	assert.ErrorContains(t, err, "exceeds")
}
