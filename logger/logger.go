package logger

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logutils"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

func SetupLogger() {
	var output io.Writer = os.Stdout
	if *common.LogDir != "" {
		logPath := filepath.Join(*common.LogDir, fmt.Sprintf("oneapi-%s.log", time.Now().Format("20060102150405")))
		fd, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			log.Fatal("failed to open log file")
		}
		output = io.MultiWriter(os.Stdout, fd)
		gin.DefaultWriter = output
		gin.DefaultErrorWriter = output
	} else {
		gin.DefaultWriter = os.Stdout
		gin.DefaultErrorWriter = os.Stderr
	}

	level := "info"
	if common.DebugEnabled {
		level = "debug"
	}
	envName := os.Getenv("APP_ENV")
	if envName == "" {
		envName = os.Getenv("NODE_TYPE")
	}
	logutils.Init(logutils.InitOptions{
		Writer:         output,
		Level:          level,
		Env:            envName,
		ServiceName:    "new-api",
		ServiceVersion: common.Version,
	})
}

func LogInfo(ctx context.Context, msg string) {
	logutils.Info(ctx).Msg(msg)
}

func LogWarn(ctx context.Context, msg string) {
	logutils.Warn(ctx).Msg(msg)
}

func LogError(ctx context.Context, msg string) {
	logutils.Error(ctx).Msg(msg)
}

func LogDebug(ctx context.Context, msg string, args ...any) {
	if common.DebugEnabled {
		if len(args) > 0 {
			msg = fmt.Sprintf(msg, args...)
		}
		logutils.Debug(ctx).Msg(msg)
	}
}

func LogQuota(quota int) string {
	// 新逻辑：根据额度展示类型输出
	q := float64(quota)
	switch operation_setting.GetQuotaDisplayType() {
	case operation_setting.QuotaDisplayTypeCNY:
		usd := q / common.QuotaPerUnit
		cny := usd * operation_setting.USDExchangeRate
		return fmt.Sprintf("¥%.6f 额度", cny)
	case operation_setting.QuotaDisplayTypeCustom:
		usd := q / common.QuotaPerUnit
		rate := operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate
		symbol := operation_setting.GetGeneralSetting().CustomCurrencySymbol
		if symbol == "" {
			symbol = "¤"
		}
		if rate <= 0 {
			rate = 1
		}
		v := usd * rate
		return fmt.Sprintf("%s%.6f 额度", symbol, v)
	case operation_setting.QuotaDisplayTypeTokens:
		return fmt.Sprintf("%d 点额度", quota)
	default: // USD
		return fmt.Sprintf("＄%.6f 额度", q/common.QuotaPerUnit)
	}
}

func FormatQuota(quota int) string {
	q := float64(quota)
	switch operation_setting.GetQuotaDisplayType() {
	case operation_setting.QuotaDisplayTypeCNY:
		usd := q / common.QuotaPerUnit
		cny := usd * operation_setting.USDExchangeRate
		return fmt.Sprintf("¥%.6f", cny)
	case operation_setting.QuotaDisplayTypeCustom:
		usd := q / common.QuotaPerUnit
		rate := operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate
		symbol := operation_setting.GetGeneralSetting().CustomCurrencySymbol
		if symbol == "" {
			symbol = "¤"
		}
		if rate <= 0 {
			rate = 1
		}
		v := usd * rate
		return fmt.Sprintf("%s%.6f", symbol, v)
	case operation_setting.QuotaDisplayTypeTokens:
		return fmt.Sprintf("%d", quota)
	default:
		return fmt.Sprintf("＄%.6f", q/common.QuotaPerUnit)
	}
}

// LogJson 仅供测试使用 only for test
func LogJson(ctx context.Context, msg string, obj any) {
	jsonStr, err := json.Marshal(obj)
	if err != nil {
		LogError(ctx, fmt.Sprintf("json marshal failed: %s", err.Error()))
		return
	}
	LogDebug(ctx, fmt.Sprintf("%s | %s", msg, string(jsonStr)))
}
