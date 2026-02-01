package controller

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/samber/lo"

	"github.com/gin-gonic/gin"
)

type testResult struct {
	context     *gin.Context
	localErr    error
	newAPIError *types.NewAPIError
}

var errSkipChannelTest = errors.New("skip channel test: no test model")

func recordFailedChannelTestLog(c *gin.Context, channel *model.Channel, modelName string, startedAt time.Time, other map[string]any, reason string) {
	if c == nil || channel == nil {
		return
	}
	useTimeSeconds := int(time.Since(startedAt).Seconds())
	if useTimeSeconds < 0 {
		useTimeSeconds = 0
	}
	if other == nil {
		other = map[string]any{}
	}
	if reason != "" {
		other["error"] = reason
	}
	model.RecordConsumeLog(c, 1, model.RecordConsumeLogParams{
		ChannelId:        channel.Id,
		PromptTokens:     0,
		CompletionTokens: 0,
		ModelName:        modelName,
		TokenName:        "模型测试",
		Quota:            0,
		Content:          "模型测试（失败）",
		UseTimeSeconds:   useTimeSeconds,
		IsStream:         false,
		Group:            c.GetString("group"),
		Other:            other,
	})
}

func testChannel(channel *model.Channel, testModel string, endpointType string) testResult {
	tik := time.Now()
	var recordedConsumeLog bool
	var logOther map[string]any
	var unsupportedTestChannelTypes = []int{
		constant.ChannelTypeMidjourney,
		constant.ChannelTypeMidjourneyPlus,
		constant.ChannelTypeSunoAPI,
		constant.ChannelTypeKling,
		constant.ChannelTypeJimeng,
		constant.ChannelTypeDoubaoVideo,
		constant.ChannelTypeVidu,
	}
	if lo.Contains(unsupportedTestChannelTypes, channel.Type) {
		channelTypeName := constant.GetChannelTypeName(channel.Type)
		return testResult{
			localErr: fmt.Errorf("%s channel test is not supported", channelTypeName),
		}
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	testModel = strings.TrimSpace(testModel)
	if testModel == "" {
		if channel.TestModel != nil && *channel.TestModel != "" {
			testModel = strings.TrimSpace(*channel.TestModel)
		} else {
			return testResult{localErr: errSkipChannelTest}
		}
	}
	defer func() {
		if recordedConsumeLog {
			return
		}
		recordFailedChannelTestLog(c, channel, testModel, tik, logOther, "")
	}()

	requestPath := "/v1/chat/completions"

	// 如果指定了端点类型，使用指定的端点类型
	if endpointType != "" {
		if endpointInfo, ok := common.GetDefaultEndpointInfo(constant.EndpointType(endpointType)); ok {
			requestPath = endpointInfo.Path
		}
	} else {
		// 如果没有指定端点类型，使用原有的自动检测逻辑
		// 先判断是否为 Embedding 模型
		if strings.Contains(strings.ToLower(testModel), "embedding") ||
			strings.HasPrefix(testModel, "m3e") || // m3e 系列模型
			strings.Contains(testModel, "bge-") || // bge 系列模型
			strings.Contains(testModel, "embed") ||
			channel.Type == constant.ChannelTypeMokaAI { // 其他 embedding 模型
			requestPath = "/v1/embeddings" // 修改请求路径
		}

		// VolcEngine 图像生成模型
		if channel.Type == constant.ChannelTypeVolcEngine && strings.Contains(testModel, "seedream") {
			requestPath = "/v1/images/generations"
		}

		// responses-only models
		if strings.Contains(strings.ToLower(testModel), "codex") {
			requestPath = "/v1/responses"
		}
	}

	c.Request = &http.Request{
		Method: "POST",
		URL:    &url.URL{Path: requestPath}, // 使用动态路径
		Body:   nil,
		Header: make(http.Header),
	}

	cache, err := model.GetUserCache(1)
	if err != nil {
		recordFailedChannelTestLog(c, channel, testModel, tik, logOther, err.Error())
		recordedConsumeLog = true
		return testResult{
			localErr:    err,
			newAPIError: nil,
		}
	}
	cache.WriteContext(c)

	//c.Request.Header.Set("Authorization", "Bearer "+channel.Key)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("channel", channel.Type)
	c.Set("base_url", channel.GetBaseURL())
	group, _ := model.GetUserGroup(1, false)
	c.Set("group", group)

	newAPIError := middleware.SetupContextForSelectedChannel(c, channel, testModel)
	if newAPIError != nil {
		recordFailedChannelTestLog(c, channel, testModel, tik, logOther, newAPIError.Error())
		recordedConsumeLog = true
		return testResult{
			context:     c,
			localErr:    newAPIError,
			newAPIError: newAPIError,
		}
	}

	// Determine relay format based on endpoint type or request path
	var relayFormat types.RelayFormat
	if endpointType != "" {
		// 根据指定的端点类型设置 relayFormat
		switch constant.EndpointType(endpointType) {
		case constant.EndpointTypeOpenAI:
			relayFormat = types.RelayFormatOpenAI
		case constant.EndpointTypeOpenAIResponse:
			relayFormat = types.RelayFormatOpenAIResponses
		case constant.EndpointTypeAnthropic:
			relayFormat = types.RelayFormatClaude
		case constant.EndpointTypeGemini:
			relayFormat = types.RelayFormatGemini
		case constant.EndpointTypeJinaRerank:
			relayFormat = types.RelayFormatRerank
		case constant.EndpointTypeImageGeneration:
			relayFormat = types.RelayFormatOpenAIImage
		case constant.EndpointTypeEmbeddings:
			relayFormat = types.RelayFormatEmbedding
		default:
			relayFormat = types.RelayFormatOpenAI
		}
	} else {
		// 根据请求路径自动检测
		relayFormat = types.RelayFormatOpenAI
		if c.Request.URL.Path == "/v1/embeddings" {
			relayFormat = types.RelayFormatEmbedding
		}
		if c.Request.URL.Path == "/v1/images/generations" {
			relayFormat = types.RelayFormatOpenAIImage
		}
		if c.Request.URL.Path == "/v1/messages" {
			relayFormat = types.RelayFormatClaude
		}
		if strings.Contains(c.Request.URL.Path, "/v1beta/models") {
			relayFormat = types.RelayFormatGemini
		}
		if c.Request.URL.Path == "/v1/rerank" || c.Request.URL.Path == "/rerank" {
			relayFormat = types.RelayFormatRerank
		}
		if c.Request.URL.Path == "/v1/responses" {
			relayFormat = types.RelayFormatOpenAIResponses
		}
	}

	request := buildTestRequest(testModel, endpointType, channel)
	if testRequestBody, err := model.GetModelTestRequestBodyByName(testModel); err != nil {
		recordFailedChannelTestLog(c, channel, testModel, tik, logOther, err.Error())
		recordedConsumeLog = true
		return testResult{context: c, localErr: err, newAPIError: types.NewError(err, types.ErrorCodeInvalidRequest)}
	} else if testRequestBody != nil && strings.TrimSpace(*testRequestBody) != "" {
		overridden, err := parseTestRequestOverride(*testRequestBody, testModel, relayFormat)
		if err != nil {
			recordFailedChannelTestLog(c, channel, testModel, tik, logOther, err.Error())
			recordedConsumeLog = true
			return testResult{context: c, localErr: err, newAPIError: types.NewError(err, types.ErrorCodeInvalidRequest)}
		}
		request = overridden
	}

	info, err := relaycommon.GenRelayInfo(c, relayFormat, request, nil)

	if err != nil {
		recordFailedChannelTestLog(c, channel, testModel, tik, logOther, err.Error())
		recordedConsumeLog = true
		return testResult{
			context:     c,
			localErr:    err,
			newAPIError: types.NewError(err, types.ErrorCodeGenRelayInfoFailed),
		}
	}

	info.IsChannelTest = true
	info.InitChannelMeta(c)

	err = helper.ModelMappedHelper(c, info, request)
	if err != nil {
		recordFailedChannelTestLog(c, channel, testModel, tik, logOther, err.Error())
		recordedConsumeLog = true
		return testResult{
			context:     c,
			localErr:    err,
			newAPIError: types.NewError(err, types.ErrorCodeChannelModelMappedError),
		}
	}

	testModel = info.UpstreamModelName
	// 更新请求中的模型名称
	request.SetModelName(testModel)

	apiType, _ := common.ChannelType2APIType(channel.Type)
	adaptor := relay.GetAdaptor(apiType)
	if adaptor == nil {
		recordFailedChannelTestLog(c, channel, testModel, tik, logOther, fmt.Sprintf("invalid api type: %d, adaptor is nil", apiType))
		recordedConsumeLog = true
		return testResult{
			context:     c,
			localErr:    fmt.Errorf("invalid api type: %d, adaptor is nil", apiType),
			newAPIError: types.NewError(fmt.Errorf("invalid api type: %d, adaptor is nil", apiType), types.ErrorCodeInvalidApiType),
		}
	}

	//// 创建一个用于日志的 info 副本，移除 ApiKey
	//logInfo := info
	//logInfo.ApiKey = ""
	common.SysLog(fmt.Sprintf("testing channel %d with model %s , info %+v ", channel.Id, testModel, info.ToString()))

	priceData, err := helper.ModelPriceHelper(c, info, 0, request.GetTokenCountMeta())
	if err != nil {
		recordFailedChannelTestLog(c, channel, testModel, tik, logOther, err.Error())
		recordedConsumeLog = true
		return testResult{
			context:     c,
			localErr:    err,
			newAPIError: types.NewError(err, types.ErrorCodeModelPriceError),
		}
	}

	adaptor.Init(info)

	var convertedRequest any
	// 根据 RelayMode 选择正确的转换函数
	switch info.RelayMode {
	case relayconstant.RelayModeEmbeddings:
		// Embedding 请求 - request 已经是正确的类型
		if embeddingReq, ok := request.(*dto.EmbeddingRequest); ok {
			convertedRequest, err = adaptor.ConvertEmbeddingRequest(c, info, *embeddingReq)
		} else {
			return testResult{
				context:     c,
				localErr:    errors.New("invalid embedding request type"),
				newAPIError: types.NewError(errors.New("invalid embedding request type"), types.ErrorCodeConvertRequestFailed),
			}
		}
	case relayconstant.RelayModeImagesGenerations:
		// 图像生成请求 - request 已经是正确的类型
		if imageReq, ok := request.(*dto.ImageRequest); ok {
			convertedRequest, err = adaptor.ConvertImageRequest(c, info, *imageReq)
		} else {
			return testResult{
				context:     c,
				localErr:    errors.New("invalid image request type"),
				newAPIError: types.NewError(errors.New("invalid image request type"), types.ErrorCodeConvertRequestFailed),
			}
		}
	case relayconstant.RelayModeRerank:
		// Rerank 请求 - request 已经是正确的类型
		if rerankReq, ok := request.(*dto.RerankRequest); ok {
			convertedRequest, err = adaptor.ConvertRerankRequest(c, info.RelayMode, *rerankReq)
		} else {
			return testResult{
				context:     c,
				localErr:    errors.New("invalid rerank request type"),
				newAPIError: types.NewError(errors.New("invalid rerank request type"), types.ErrorCodeConvertRequestFailed),
			}
		}
	case relayconstant.RelayModeResponses:
		// Response 请求 - request 已经是正确的类型
		if responseReq, ok := request.(*dto.OpenAIResponsesRequest); ok {
			convertedRequest, err = adaptor.ConvertOpenAIResponsesRequest(c, info, *responseReq)
		} else {
			return testResult{
				context:     c,
				localErr:    errors.New("invalid response request type"),
				newAPIError: types.NewError(errors.New("invalid response request type"), types.ErrorCodeConvertRequestFailed),
			}
		}
	default:
		// Chat/Completion 等其他请求类型
		if generalReq, ok := request.(*dto.GeneralOpenAIRequest); ok {
			convertedRequest, err = adaptor.ConvertOpenAIRequest(c, info, generalReq)
		} else {
			return testResult{
				context:     c,
				localErr:    errors.New("invalid general request type"),
				newAPIError: types.NewError(errors.New("invalid general request type"), types.ErrorCodeConvertRequestFailed),
			}
		}
	}

	if err != nil {
		recordFailedChannelTestLog(c, channel, testModel, tik, logOther, err.Error())
		recordedConsumeLog = true
		return testResult{
			context:     c,
			localErr:    err,
			newAPIError: types.NewError(err, types.ErrorCodeConvertRequestFailed),
		}
	}
	jsonData, err := json.Marshal(convertedRequest)
	if err != nil {
		recordFailedChannelTestLog(c, channel, testModel, tik, logOther, err.Error())
		recordedConsumeLog = true
		return testResult{
			context:     c,
			localErr:    err,
			newAPIError: types.NewError(err, types.ErrorCodeJsonMarshalFailed),
		}
	}

	//jsonData, err = relaycommon.RemoveDisabledFields(jsonData, info.ChannelOtherSettings)
	//if err != nil {
	//	return testResult{
	//		context:     c,
	//		localErr:    err,
	//		newAPIError: types.NewError(err, types.ErrorCodeConvertRequestFailed),
	//	}
	//}

	if len(info.ParamOverride) > 0 {
		jsonData, err = relaycommon.ApplyParamOverride(jsonData, info.ParamOverride, relaycommon.BuildParamOverrideContext(info))
		if err != nil {
			recordFailedChannelTestLog(c, channel, testModel, tik, logOther, err.Error())
			recordedConsumeLog = true
			return testResult{
				context:     c,
				localErr:    err,
				newAPIError: types.NewError(err, types.ErrorCodeChannelParamOverrideInvalid),
			}
		}
	}

	requestBody := bytes.NewBuffer(jsonData)
	c.Request.Body = io.NopCloser(requestBody)
	resp, err := adaptor.DoRequest(c, info, requestBody)
	if err != nil {
		recordFailedChannelTestLog(c, channel, testModel, tik, logOther, err.Error())
		recordedConsumeLog = true
		return testResult{
			context:     c,
			localErr:    err,
			newAPIError: types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError),
		}
	}
	var httpResp *http.Response
	if resp != nil {
		httpResp = resp.(*http.Response)
		if httpResp.StatusCode != http.StatusOK {
			err := service.RelayErrorHandler(c.Request.Context(), httpResp, true)
			common.SysError(fmt.Sprintf(
				"channel test bad response: channel_id=%d name=%s type=%d model=%s endpoint_type=%s status=%d err=%v",
				channel.Id,
				channel.Name,
				channel.Type,
				testModel,
				endpointType,
				httpResp.StatusCode,
				err,
			))
			recordFailedChannelTestLog(c, channel, testModel, tik, logOther, err.Error())
			recordedConsumeLog = true
			return testResult{
				context:     c,
				localErr:    err,
				newAPIError: types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError),
			}
		}
	}
	usageA, respErr := adaptor.DoResponse(c, httpResp, info)
	if respErr != nil {
		recordFailedChannelTestLog(c, channel, testModel, tik, logOther, respErr.Error())
		recordedConsumeLog = true
		return testResult{
			context:     c,
			localErr:    respErr,
			newAPIError: respErr,
		}
	}
	if usageA == nil {
		recordFailedChannelTestLog(c, channel, testModel, tik, logOther, "usage is nil")
		recordedConsumeLog = true
		return testResult{
			context:     c,
			localErr:    errors.New("usage is nil"),
			newAPIError: types.NewOpenAIError(errors.New("usage is nil"), types.ErrorCodeBadResponseBody, http.StatusInternalServerError),
		}
	}
	usage := usageA.(*dto.Usage)
	result := w.Result()
	respBody, err := io.ReadAll(result.Body)
	if err != nil {
		recordFailedChannelTestLog(c, channel, testModel, tik, logOther, err.Error())
		recordedConsumeLog = true
		return testResult{
			context:     c,
			localErr:    err,
			newAPIError: types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError),
		}
	}
	info.SetEstimatePromptTokens(usage.PromptTokens)

	quota := 0
	if !priceData.UsePrice {
		quota = usage.PromptTokens + int(math.Round(float64(usage.CompletionTokens)*priceData.CompletionRatio))
		quota = int(math.Round(float64(quota) * priceData.ModelRatio))
		if priceData.ModelRatio != 0 && quota <= 0 {
			quota = 1
		}
	} else {
		quota = int(priceData.ModelPrice * common.QuotaPerUnit)
	}
	tok := time.Now()
	milliseconds := tok.Sub(tik).Milliseconds()
	consumedTime := float64(milliseconds) / 1000.0
	other := service.GenerateTextOtherInfo(c, info, priceData.ModelRatio, priceData.GroupRatioInfo.GroupRatio, priceData.CompletionRatio,
		usage.PromptTokensDetails.CachedTokens, priceData.CacheRatio, priceData.ModelPrice, priceData.GroupRatioInfo.GroupSpecialRatio)
	model.RecordConsumeLog(c, 1, model.RecordConsumeLogParams{
		ChannelId:        channel.Id,
		PromptTokens:     usage.PromptTokens,
		CompletionTokens: usage.CompletionTokens,
		ModelName:        info.OriginModelName,
		TokenName:        "模型测试",
		Quota:            quota,
		Content:          "模型测试",
		UseTimeSeconds:   int(consumedTime),
		IsStream:         info.IsStream,
		Group:            info.UsingGroup,
		Other:            other,
	})
	recordedConsumeLog = true
	common.SysLog(fmt.Sprintf("testing channel #%d, response: \n%s", channel.Id, string(respBody)))
	return testResult{
		context:     c,
		localErr:    nil,
		newAPIError: nil,
	}
}

func parseTestRequestOverride(body string, testModel string, relayFormat types.RelayFormat) (dto.Request, error) {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return nil, errors.New("empty test_request_body")
	}

	switch relayFormat {
	case types.RelayFormatOpenAI, types.RelayFormatClaude, types.RelayFormatGemini:
		var req dto.GeneralOpenAIRequest
		if err := json.Unmarshal([]byte(trimmed), &req); err != nil {
			return nil, err
		}
		req.Model = testModel
		return &req, nil
	case types.RelayFormatEmbedding:
		var req dto.EmbeddingRequest
		if err := json.Unmarshal([]byte(trimmed), &req); err != nil {
			return nil, err
		}
		req.Model = testModel
		return &req, nil
	case types.RelayFormatOpenAIImage:
		var req dto.ImageRequest
		if err := json.Unmarshal([]byte(trimmed), &req); err != nil {
			return nil, err
		}
		req.Model = testModel
		return &req, nil
	case types.RelayFormatRerank:
		var req dto.RerankRequest
		if err := json.Unmarshal([]byte(trimmed), &req); err != nil {
			return nil, err
		}
		req.Model = testModel
		return &req, nil
	case types.RelayFormatOpenAIResponses:
		var req dto.OpenAIResponsesRequest
		if err := json.Unmarshal([]byte(trimmed), &req); err != nil {
			return nil, err
		}
		req.Model = testModel
		return &req, nil
	default:
		return nil, fmt.Errorf("unsupported relay format for channel test override: %v", relayFormat)
	}
}

func buildTestRequest(modelName string, endpointType string, channel *model.Channel) dto.Request {
	// Keep signature unchanged for callers, but generate request strictly based on
	// the resolved request path (and thus relay mode/format), to stay consistent
	// with the downstream validation/convert logic.
	requestPath := "/v1/chat/completions"

	if endpointType != "" {
		if endpointInfo, ok := common.GetDefaultEndpointInfo(constant.EndpointType(endpointType)); ok {
			requestPath = endpointInfo.Path
		}
	} else {
		lower := strings.ToLower(modelName)
		if strings.Contains(lower, "embedding") ||
			strings.HasPrefix(modelName, "m3e") ||
			strings.Contains(modelName, "bge-") ||
			strings.Contains(modelName, "embed") ||
			(channel != nil && channel.Type == constant.ChannelTypeMokaAI) {
			requestPath = "/v1/embeddings"
		}
		if channel != nil && channel.Type == constant.ChannelTypeVolcEngine && strings.Contains(modelName, "seedream") {
			requestPath = "/v1/images/generations"
		}
		if strings.Contains(lower, "codex") {
			requestPath = "/v1/responses"
		}
	}

	relayMode := relayconstant.Path2RelayMode(requestPath)

	switch relayMode {
	case relayconstant.RelayModeEmbeddings:
		return &dto.EmbeddingRequest{Model: modelName, Input: []any{"hello world"}}
	case relayconstant.RelayModeImagesGenerations:
		return &dto.ImageRequest{Model: modelName, Prompt: "a cute cat", N: 1, Size: "1024x1024"}
	case relayconstant.RelayModeRerank:
		return &dto.RerankRequest{
			Model:     modelName,
			Query:     "What is Deep Learning?",
			Documents: []any{"Deep Learning is a subset of machine learning.", "Machine learning is a field of artificial intelligence."},
			TopN:      2,
		}
	case relayconstant.RelayModeResponses:
		return &dto.OpenAIResponsesRequest{Model: modelName, Input: json.RawMessage("\"hi\"")}
	default:
		// Chat/Completion 请求 - 返回 GeneralOpenAIRequest
		testRequest := &dto.GeneralOpenAIRequest{
			Model:  modelName,
			Stream: false,
			Messages: []dto.Message{{
				Role:    "user",
				Content: "hi",
			}},
		}

		if strings.HasPrefix(modelName, "o") {
			testRequest.MaxCompletionTokens = 16
		} else if strings.Contains(modelName, "thinking") {
			if !strings.Contains(modelName, "claude") {
				testRequest.MaxTokens = 50
			}
		} else if strings.Contains(modelName, "gemini") {
			testRequest.MaxTokens = 3000
		} else {
			testRequest.MaxTokens = 16
		}

		return testRequest
	}
}

func TestChannel(c *gin.Context) {
	channelId, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	channel, err := model.CacheGetChannel(channelId)
	if err != nil {
		channel, err = model.GetChannelById(channelId, true)
		if err != nil {
			common.ApiError(c, err)
			return
		}
	}
	//defer func() {
	//	if channel.ChannelInfo.IsMultiKey {
	//		go func() { _ = channel.SaveChannelInfo() }()
	//	}
	//}()
	testModel := c.Query("model")
	endpointType := c.Query("endpoint_type")
	tik := time.Now()
	result := testChannel(channel, testModel, endpointType)
	if result.localErr != nil {
		if errors.Is(result.localErr, errSkipChannelTest) {
			c.JSON(http.StatusOK, gin.H{
				"success": true,
				"message": "",
				"time":    0.0,
				"skipped": true,
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": result.localErr.Error(),
			"time":    0.0,
		})
		return
	}
	tok := time.Now()
	milliseconds := tok.Sub(tik).Milliseconds()
	go channel.UpdateResponseTime(milliseconds)
	consumedTime := float64(milliseconds) / 1000.0
	if result.newAPIError != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": result.newAPIError.Error(),
			"time":    consumedTime,
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"time":    consumedTime,
	})
}

var testAllChannelsLock sync.Mutex
var testAllChannelsRunning bool = false

func testAllChannels(notify bool) error {

	testAllChannelsLock.Lock()
	if testAllChannelsRunning {
		testAllChannelsLock.Unlock()
		return errors.New("测试已在运行中")
	}
	testAllChannelsRunning = true
	testAllChannelsLock.Unlock()
	channels, getChannelErr := model.GetAllChannels(0, 0, true, false)
	if getChannelErr != nil {
		return getChannelErr
	}
	var disableThreshold = int64(common.ChannelDisableThreshold * 1000)
	if disableThreshold == 0 {
		disableThreshold = 10000000 // a impossible value
	}
	gopool.Go(func() {
		// 使用 defer 确保无论如何都会重置运行状态，防止死锁
		defer func() {
			testAllChannelsLock.Lock()
			testAllChannelsRunning = false
			testAllChannelsLock.Unlock()
		}()

		for _, channel := range channels {
			isChannelEnabled := channel.Status == common.ChannelStatusEnabled
			tik := time.Now()
			result := testChannel(channel, "", "")
			if errors.Is(result.localErr, errSkipChannelTest) {
				continue
			}
			tok := time.Now()
			milliseconds := tok.Sub(tik).Milliseconds()

			shouldBanChannel := false
			newAPIError := result.newAPIError
			// request error disables the channel
			if newAPIError != nil {
				shouldBanChannel = service.ShouldDisableChannel(channel.Type, result.newAPIError)
			}

			// 当错误检查通过，才检查响应时间
			if common.AutomaticDisableChannelEnabled && !shouldBanChannel {
				if milliseconds > disableThreshold {
					err := fmt.Errorf("响应时间 %.2fs 超过阈值 %.2fs", float64(milliseconds)/1000.0, float64(disableThreshold)/1000.0)
					newAPIError = types.NewOpenAIError(err, types.ErrorCodeChannelResponseTimeExceeded, http.StatusRequestTimeout)
					shouldBanChannel = true
				}
			}

			// disable channel
			if isChannelEnabled && shouldBanChannel && channel.GetAutoBan() {
				processChannelError(result.context, *types.NewChannelError(channel.Id, channel.Type, channel.Name, channel.ChannelInfo.IsMultiKey, common.GetContextKeyString(result.context, constant.ContextKeyChannelKey), channel.GetAutoBan()), newAPIError)
			}

			// enable channel
			if !isChannelEnabled && service.ShouldEnableChannel(channel.Status, shouldBanChannel) {
				service.EnableChannel(channel.Id, common.GetContextKeyString(result.context, constant.ContextKeyChannelKey), channel.Name)
			}

			channel.UpdateResponseTime(milliseconds)
			time.Sleep(common.RequestInterval)
		}

		if notify {
			service.NotifyRootUser(dto.NotifyTypeChannelTest, "通道测试完成", "所有通道测试已完成")
		}
	})
	return nil
}

func TestAllChannels(c *gin.Context) {
	err := testAllChannels(true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
}

var autoTestChannelsOnce sync.Once

func AutomaticallyTestChannels() {
	// 只在Master节点定时测试渠道
	if !common.IsMasterNode {
		return
	}
	autoTestChannelsOnce.Do(func() {
		for {
			if !operation_setting.GetMonitorSetting().AutoTestChannelEnabled {
				time.Sleep(1 * time.Minute)
				continue
			}
			for {
				frequency := operation_setting.GetMonitorSetting().AutoTestChannelMinutes
				time.Sleep(time.Duration(int(math.Round(frequency))) * time.Minute)
				common.SysLog(fmt.Sprintf("automatically test channels with interval %f minutes", frequency))
				common.SysLog("automatically testing all channels")
				_ = testAllChannels(false)
				common.SysLog("automatically channel test finished")
				if !operation_setting.GetMonitorSetting().AutoTestChannelEnabled {
					break
				}
			}
		}
	})
}
