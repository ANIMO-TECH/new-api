package logger

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGinWriterContinuesAfterRotation(t *testing.T) {
	t.Setenv("LOG_ROTATION_ENABLED", "true")
	dir := t.TempDir()
	oldDir, oldOut, oldErr := common.LogDir, gin.DefaultWriter, gin.DefaultErrorWriter
	common.LogDir = &dir
	t.Cleanup(func() {
		if rotatingLogFile != nil {
			require.NoError(t, rotatingLogFile.Close())
			rotatingLogFile = nil
		}
		rotationEnabled.Store(false)
		if currentLogFile != nil {
			_ = currentLogFile.Close()
		}
		common.LogDir = oldDir
		gin.DefaultWriter, gin.DefaultErrorWriter = oldOut, oldErr
	})
	SetupLogger()
	server := gin.New()
	server.Use(gin.LoggerWithWriter(gin.DefaultWriter))
	server.GET("/:phase", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	request := func(path string) {
		t.Helper()
		response := httptest.NewRecorder()
		server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusNoContent, response.Code)
	}
	request("/before-rotation")
	SetupLogger()
	request("/after-rotation")
	files, err := filepath.Glob(filepath.Join(dir, "oneapi-*.log"))
	require.NoError(t, err)
	var output strings.Builder
	for _, name := range files {
		f, err := os.Open(name)
		require.NoError(t, err)
		_, err = io.Copy(&output, f)
		require.NoError(t, err)
		require.NoError(t, f.Close())
	}
	assert.Contains(t, output.String(), "/before-rotation")
	assert.Contains(t, output.String(), "/after-rotation", "Gin keeps its writer; rotating must not strand request logs on a closed descriptor")
}
