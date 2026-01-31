package controller

import (
	"encoding/json"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// GetModelTestRequestTemplate returns the default test request body for a model.
// It is used by frontend to prefill test request override editor.
func GetModelTestRequestTemplate(c *gin.Context) {
	modelName := strings.TrimSpace(c.Query("model_name"))
	if modelName == "" {
		common.ApiErrorMsg(c, "缺少 model_name")
		return
	}
	endpointType := strings.TrimSpace(c.Query("endpoint_type"))

	// channel is only used for a few special-cases in buildTestRequest
	request := buildTestRequest(modelName, endpointType, &model.Channel{})

	jsonBytes, err := json.MarshalIndent(request, "", "  ")
	if err != nil {
		common.ApiError(c, err)
		return
	}

	common.ApiSuccess(c, gin.H{
		"test_request_body": string(jsonBytes),
		"type":              requestTypeName(request),
	})
}

func requestTypeName(request dto.Request) string {
	switch request.(type) {
	case *dto.GeneralOpenAIRequest:
		return "chat_completions"
	case *dto.EmbeddingRequest:
		return "embeddings"
	case *dto.ImageRequest:
		return "images"
	case *dto.RerankRequest:
		return "rerank"
	case *dto.OpenAIResponsesRequest:
		return "responses"
	default:
		return "unknown"
	}
}
