package server

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	defaultListPageSize = 25
	maxListPageSize     = 100
	maxAuditListFetch   = 5000
)

type listPageParams struct {
	Page     int
	PageSize int
}

func parseListPageParams(r *http.Request) listPageParams {
	q := r.URL.Query()
	page := 1
	if raw := q.Get("page"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			page = n
		}
	}
	pageSize := defaultListPageSize
	if raw := q.Get("page_size"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			pageSize = n
		}
	} else if raw := q.Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			pageSize = n
		}
	}
	if pageSize > maxListPageSize {
		pageSize = maxListPageSize
	}
	return listPageParams{Page: page, PageSize: pageSize}
}

func pageBounds(total int, params listPageParams) (int, int) {
	start := (params.Page - 1) * params.PageSize
	if start > total {
		start = total
	}
	end := start + params.PageSize
	if end > total {
		end = total
	}
	return start, end
}

func listMeta(count int, total int, params listPageParams) map[string]any {
	return map[string]any{
		"count":     count,
		"total":     total,
		"page":      params.Page,
		"page_size": params.PageSize,
	}
}

func containsFold(value string, query string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	query = strings.ToLower(strings.TrimSpace(query))
	return query == "" || strings.Contains(value, query)
}

func compareInts(a int, b int) int {
	if a == b {
		return 0
	}
	if a < b {
		return -1
	}
	return 1
}

func compareInts64(a int64, b int64) int {
	if a == b {
		return 0
	}
	if a < b {
		return -1
	}
	return 1
}

func parseOptionalRFC3339(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339, value)
	return parsed, err == nil
}
