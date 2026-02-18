package common

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/QuantumNous/new-api/logutils"
	"github.com/gin-gonic/gin"
)

func SysLog(s string) {
	logutils.Info(context.Background()).
		Str("log_source", "sys").
		Msg(s)
}

func SysError(s string) {
	logutils.Error(context.Background()).
		Str("log_source", "sys").
		Msg(s)
}

func FatalLog(v ...any) {
	logutils.Error(context.Background()).
		Str("log_source", "sys").
		Msg(fmt.Sprint(v...))
	os.Exit(1)
}

func LogStartupSuccess(startTime time.Time, port string) {

	duration := time.Since(startTime)
	durationMs := duration.Milliseconds()

	// Get network IPs
	networkIps := GetNetworkIps()

	// Print blank line for spacing
	fmt.Fprintf(gin.DefaultWriter, "\n")

	// Print the main success message
	fmt.Fprintf(gin.DefaultWriter, "  \033[32m%s %s\033[0m  ready in %d ms\n", SystemName, Version, durationMs)
	fmt.Fprintf(gin.DefaultWriter, "\n")

	// Skip fancy startup message in container environments
	if !IsRunningInContainer() {
		// Print local URL
		fmt.Fprintf(gin.DefaultWriter, "  ➜  \033[1mLocal:\033[0m   http://localhost:%s/\n", port)
	}

	// Print network URLs
	for _, ip := range networkIps {
		fmt.Fprintf(gin.DefaultWriter, "  ➜  \033[1mNetwork:\033[0m http://%s:%s/\n", ip, port)
	}

	// Print blank line for spacing
	fmt.Fprintf(gin.DefaultWriter, "\n")
}
