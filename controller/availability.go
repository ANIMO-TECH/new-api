package controller

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// GetAvailabilitySummary outputs an LLM call availability report (markdown) for the last N days.
// Reads the logs table directly (type=2 success / type=5 error),
// aggregates by model, channel, model x channel, error pattern.
// Mounted under /log with AdminAuth middleware (see router/api-router.go).
//
// Query: ?days=7 (1..90, default 7)
func GetAvailabilitySummary(c *gin.Context) {
	days := 7
	if v := c.Query("days"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 90 {
			days = n
		}
	}
	since := time.Now().AddDate(0, 0, -days).Unix()

	// 1. by model
	type modelRow struct {
		ModelName string  `gorm:"column:model_name"`
		Total     int64   `gorm:"column:total"`
		Ok        int64   `gorm:"column:ok"`
		Err       int64   `gorm:"column:err"`
		Pct       float64 `gorm:"column:pct"`
		AvgMs     float64 `gorm:"column:avg_ms"`
	}
	var modelRows []modelRow
	model.LOG_DB.Raw(`
		SELECT model_name,
		       COUNT(*) AS total,
		       SUM(CASE WHEN type = 2 THEN 1 ELSE 0 END) AS ok,
		       SUM(CASE WHEN type = 5 THEN 1 ELSE 0 END) AS err,
		       ROUND(SUM(CASE WHEN type = 2 THEN 1 ELSE 0 END) * 100.0 / NULLIF(COUNT(*), 0), 2) AS pct,
		       ROUND(AVG(CASE WHEN type = 2 THEN use_time END), 0) AS avg_ms
		FROM logs
		WHERE created_at >= ?
		  AND type IN (2, 5)
		  AND model_name <> ''
		GROUP BY model_name
		ORDER BY err DESC, pct ASC
		LIMIT 50
	`, since).Scan(&modelRows)

	// 2. by channel
	type chanRow struct {
		ChannelId int64   `gorm:"column:channel_id"`
		Total     int64   `gorm:"column:total"`
		Ok        int64   `gorm:"column:ok"`
		Err       int64   `gorm:"column:err"`
		Pct       float64 `gorm:"column:pct"`
	}
	var chanRows []chanRow
	model.LOG_DB.Raw(`
		SELECT channel_id,
		       COUNT(*) AS total,
		       SUM(CASE WHEN type = 2 THEN 1 ELSE 0 END) AS ok,
		       SUM(CASE WHEN type = 5 THEN 1 ELSE 0 END) AS err,
		       ROUND(SUM(CASE WHEN type = 2 THEN 1 ELSE 0 END) * 100.0 / NULLIF(COUNT(*), 0), 2) AS pct
		FROM logs
		WHERE created_at >= ?
		  AND type IN (2, 5)
		  AND channel_id > 0
		GROUP BY channel_id
		HAVING COUNT(*) >= 10
		ORDER BY err DESC, pct ASC
		LIMIT 30
	`, since).Scan(&chanRows)

	// 3. worst model x channel combos
	type comboRow struct {
		ModelName string  `gorm:"column:model_name"`
		ChannelId int64   `gorm:"column:channel_id"`
		Total     int64   `gorm:"column:total"`
		Err       int64   `gorm:"column:err"`
		Pct       float64 `gorm:"column:pct"`
	}
	var comboRows []comboRow
	model.LOG_DB.Raw(`
		SELECT model_name,
		       channel_id,
		       COUNT(*) AS total,
		       SUM(CASE WHEN type = 5 THEN 1 ELSE 0 END) AS err,
		       ROUND(SUM(CASE WHEN type = 2 THEN 1 ELSE 0 END) * 100.0 / NULLIF(COUNT(*), 0), 2) AS pct
		FROM logs
		WHERE created_at >= ?
		  AND type IN (2, 5)
		  AND model_name <> ''
		GROUP BY model_name, channel_id
		HAVING SUM(CASE WHEN type = 5 THEN 1 ELSE 0 END) > 0
		   AND COUNT(*) >= 20
		ORDER BY err DESC
		LIMIT 30
	`, since).Scan(&comboRows)

	// 4. top error contents (truncated to 200 chars to collapse near-duplicates)
	type errRow struct {
		Content string `gorm:"column:content"`
		Cnt     int64  `gorm:"column:cnt"`
	}
	var errRows []errRow
	model.LOG_DB.Raw(`
		SELECT SUBSTR(content, 1, 200) AS content,
		       COUNT(*) AS cnt
		FROM logs
		WHERE created_at >= ?
		  AND type = 5
		  AND content <> ''
		GROUP BY SUBSTR(content, 1, 200)
		ORDER BY cnt DESC
		LIMIT 20
	`, since).Scan(&errRows)

	// assemble markdown
	var b strings.Builder
	fmt.Fprintf(&b, "# new-api Model Availability (last %d days)\n\n", days)
	fmt.Fprintf(&b, "_since: %s_\n\n", time.Unix(since, 0).Format("2006-01-02 15:04:05"))

	b.WriteString("## 1. Model availability (sorted by err desc)\n\n")
	if len(modelRows) == 0 {
		b.WriteString("_no data_\n\n")
	} else {
		b.WriteString("| model | total | ok | err | success% | avg_ms |\n|---|---|---|---|---|---|\n")
		for _, r := range modelRows {
			fmt.Fprintf(&b, "| %s | %d | %d | %d | %.2f | %.0f |\n",
				escapeCell(r.ModelName), r.Total, r.Ok, r.Err, r.Pct, r.AvgMs)
		}
		b.WriteString("\n")
	}

	b.WriteString("## 2. Channel availability\n\n")
	if len(chanRows) == 0 {
		b.WriteString("_no data_\n\n")
	} else {
		b.WriteString("| channel_id | total | ok | err | success% |\n|---|---|---|---|---|\n")
		for _, r := range chanRows {
			fmt.Fprintf(&b, "| %d | %d | %d | %d | %.2f |\n",
				r.ChannelId, r.Total, r.Ok, r.Err, r.Pct)
		}
		b.WriteString("\n")
	}

	b.WriteString("## 3. Worst model x channel combos\n\n")
	if len(comboRows) == 0 {
		b.WriteString("_no data_\n\n")
	} else {
		b.WriteString("| model | channel_id | total | err | success% |\n|---|---|---|---|---|\n")
		for _, r := range comboRows {
			fmt.Fprintf(&b, "| %s | %d | %d | %d | %.2f |\n",
				escapeCell(r.ModelName), r.ChannelId, r.Total, r.Err, r.Pct)
		}
		b.WriteString("\n")
	}

	b.WriteString("## 4. Top error contents\n\n")
	if len(errRows) == 0 {
		b.WriteString("_no data_\n\n")
	} else {
		b.WriteString("| error (first 200 chars) | count |\n|---|---|\n")
		for _, r := range errRows {
			fmt.Fprintf(&b, "| %s | %d |\n", escapeCell(r.Content), r.Cnt)
		}
	}

	c.Header("Content-Type", "text/markdown; charset=utf-8")
	c.String(http.StatusOK, b.String())
}

// escapeCell makes a string safe inside a markdown table cell.
func escapeCell(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}
