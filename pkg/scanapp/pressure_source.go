package scanapp

import (
	"fmt"
	"net/http"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
	"github.com/xuxiping/port-scan-mk3/pkg/pressure"
)

func newPressureSource(policy config.PressurePolicy, limits pressure.ResponseLimits) (PressureSource, error) {
	values, err := policy.Resolve()
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 2 * time.Second}

	switch values.Kind {
	case config.PressureKindSimple:
		return pressure.NewSimpleHTTPWithLimits(values.Endpoint, client, limits)
	case config.PressureKindAuthenticated:
		return pressure.NewOAuthMultiWithLimits(pressure.OAuthConfig{
			AuthEndpoint:  values.AuthEndpoint,
			DataEndpoints: values.DataEndpoints,
			ClientID:      values.ClientID,
			ClientSecret:  values.ClientSecret,
		}, client, limits)
	case config.PressureKindDisabled:
		return nil, nil
	default:
		return nil, fmt.Errorf("unknown pressure policy kind: %d", values.Kind)
	}
}
