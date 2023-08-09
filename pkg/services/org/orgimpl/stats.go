package orgimpl

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	ExporterName = "grafana"
)

var (
	// MStatTotalUserAccountsNoRole is a metric gauge for total number of
	// user accounts with no basic role
	MStatTotalUserAccountsNoRole prometheus.Gauge

	Initialised bool = false
)

func init() {
	MStatTotalUserAccountsNoRole = prometheus.NewGauge(prometheus.GaugeOpts{
		Name:      "stat_total_user_accounts_role_none",
		Help:      "total amount of user accounts with no role",
		Namespace: ExporterName,
	})

	prometheus.MustRegister(
		MStatTotalUserAccountsNoRole,
	)
}

func (sa *Service) getUsageMetrics(ctx context.Context) (map[string]interface{}, error) {
	stats := map[string]interface{}{}

	storeStats, err := sa.store.GetUsageMetrics(ctx)
	if err != nil {
		return nil, err
	}

	stats["stats.user.role_none.count"] = storeStats.UserAccountsWithNoRole

	MStatTotalUserAccountsNoRole.Set(float64(storeStats.UserAccountsWithNoRole))

	return stats, nil
}
