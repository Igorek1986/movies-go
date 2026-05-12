package store

import (
	"context"
	"lampa-api/db/postgres"
	"time"
)

func today() string { return time.Now().Format("2006-01-02") }

// ─── Track ────────────────────────────────────────────────────────────────────

func TrackAPIUser(ip string) {
	postgres.Pool.Exec(context.Background(), `
		INSERT INTO stats_api_users (ip, date, requests)
		VALUES ($1, $2, 1)
		ON CONFLICT (ip, date) DO UPDATE SET requests = stats_api_users.requests + 1`,
		ip, today())
}

func TrackCategoryRequest(category, ip string) {
	postgres.Pool.Exec(context.Background(), `
		INSERT INTO stats_category_requests (category, ip, date, requests)
		VALUES ($1, $2, $3, 1)
		ON CONFLICT (category, ip, date) DO UPDATE SET requests = stats_category_requests.requests + 1`,
		category, ip, today())
}

func TrackMyShowsUser(login string) {
	postgres.Pool.Exec(context.Background(), `
		INSERT INTO stats_myshows_users (login, date, requests)
		VALUES ($1, $2, 1)
		ON CONFLICT (login, date) DO UPDATE SET requests = stats_myshows_users.requests + 1`,
		login, today())
}

// ─── Query ────────────────────────────────────────────────────────────────────

type StatRow struct {
	Name     string `json:"name"`
	Requests int    `json:"requests"`
}

func GetAPIUserStats(todayOnly bool) ([]StatRow, int, int) {
	var where string
	var args []any
	if todayOnly {
		where = "WHERE date = $1"
		args = append(args, today())
	}
	rows, err := postgres.Pool.Query(context.Background(),
		`SELECT ip, SUM(requests) FROM stats_api_users `+where+
			` GROUP BY ip ORDER BY SUM(requests) DESC LIMIT 100`,
		args...)
	if err != nil {
		return nil, 0, 0
	}
	defer rows.Close()
	var result []StatRow
	totalIPs, totalReqs := 0, 0
	for rows.Next() {
		var r StatRow
		if rows.Scan(&r.Name, &r.Requests) == nil {
			result = append(result, r)
			totalIPs++
			totalReqs += r.Requests
		}
	}
	return result, totalIPs, totalReqs
}

func GetCategoryStats(todayOnly bool) []StatRow {
	var where string
	var args []any
	if todayOnly {
		where = "WHERE date = $1"
		args = append(args, today())
	}
	rows, err := postgres.Pool.Query(context.Background(),
		`SELECT category, SUM(requests) FROM stats_category_requests `+where+
			` GROUP BY category ORDER BY SUM(requests) DESC LIMIT 100`,
		args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var result []StatRow
	for rows.Next() {
		var r StatRow
		if rows.Scan(&r.Name, &r.Requests) == nil {
			result = append(result, r)
		}
	}
	return result
}

func GetMyShowsStats(todayOnly bool) []StatRow {
	var where string
	var args []any
	if todayOnly {
		where = "WHERE date = $1"
		args = append(args, today())
	}
	rows, err := postgres.Pool.Query(context.Background(),
		`SELECT login, SUM(requests) FROM stats_myshows_users `+where+
			` GROUP BY login ORDER BY SUM(requests) DESC LIMIT 100`,
		args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var result []StatRow
	for rows.Next() {
		var r StatRow
		if rows.Scan(&r.Name, &r.Requests) == nil {
			result = append(result, r)
		}
	}
	return result
}
