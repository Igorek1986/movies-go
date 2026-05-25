package store

import (
	"context"
	"movies-api/db/models"
	"movies-api/db/postgres"
)

func ListProxyConfigs(ctx context.Context) ([]models.ProxyConfig, error) {
	rows, err := postgres.Pool.Query(ctx,
		`SELECT id, name, type, config, enabled, priority, created_at
		 FROM proxy_configs ORDER BY priority, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.ProxyConfig
	for rows.Next() {
		var c models.ProxyConfig
		if err := rows.Scan(&c.ID, &c.Name, &c.Type, &c.Config, &c.Enabled, &c.Priority, &c.CreatedAt); err == nil {
			out = append(out, c)
		}
	}
	return out, nil
}

func GetProxyConfig(ctx context.Context, id int) (*models.ProxyConfig, error) {
	var c models.ProxyConfig
	err := postgres.Pool.QueryRow(ctx,
		`SELECT id, name, type, config, enabled, priority, created_at
		 FROM proxy_configs WHERE id = $1`, id).
		Scan(&c.ID, &c.Name, &c.Type, &c.Config, &c.Enabled, &c.Priority, &c.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func CreateProxyConfig(ctx context.Context, c models.ProxyConfig) (*models.ProxyConfig, error) {
	err := postgres.Pool.QueryRow(ctx,
		`INSERT INTO proxy_configs (name, type, config, enabled, priority)
		 VALUES ($1, $2, $3, $4, $5) RETURNING id, created_at`,
		c.Name, c.Type, c.Config, c.Enabled, c.Priority).
		Scan(&c.ID, &c.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func UpdateProxyConfig(ctx context.Context, c models.ProxyConfig) error {
	_, err := postgres.Pool.Exec(ctx,
		`UPDATE proxy_configs SET name=$1, type=$2, config=$3, enabled=$4, priority=$5
		 WHERE id=$6`,
		c.Name, c.Type, c.Config, c.Enabled, c.Priority, c.ID)
	return err
}

func DeleteProxyConfig(ctx context.Context, id int) error {
	_, err := postgres.Pool.Exec(ctx, `DELETE FROM proxy_configs WHERE id=$1`, id)
	return err
}

func GetProxyRouting(ctx context.Context) ([]models.ProxyRoute, error) {
	rows, err := postgres.Pool.Query(ctx,
		`SELECT route, enabled, COALESCE(proxy_ids, '{}') FROM proxy_routing ORDER BY route`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.ProxyRoute
	for rows.Next() {
		var r models.ProxyRoute
		var ids []int32
		if err := rows.Scan(&r.Route, &r.Enabled, &ids); err == nil {
			r.ProxyIDs = make([]int, len(ids))
			for i, id := range ids {
				r.ProxyIDs[i] = int(id)
			}
			out = append(out, r)
		}
	}
	return out, nil
}

func SetProxyRoute(ctx context.Context, route string, enabled bool, proxyIDs []int) error {
	ids := make([]int32, len(proxyIDs))
	for i, id := range proxyIDs {
		ids[i] = int32(id)
	}
	_, err := postgres.Pool.Exec(ctx,
		`INSERT INTO proxy_routing (route, enabled, proxy_ids) VALUES ($1, $2, $3)
		 ON CONFLICT (route) DO UPDATE SET enabled=EXCLUDED.enabled, proxy_ids=EXCLUDED.proxy_ids`,
		route, enabled, ids)
	return err
}
