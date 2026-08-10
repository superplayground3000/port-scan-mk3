package scanapp

import (
	"fmt"
	"net/http"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
	"github.com/xuxiping/port-scan-mk3/pkg/pressure"
)

func newPressureSource(policy config.PressurePolicy) (PressureSource, error) {
	values, err := policy.Resolve()
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 2 * time.Second}

	switch values.Kind {
	case config.PressureKindSimple:
		return pressure.NewSimpleHTTP(values.Endpoint, client)
	case config.PressureKindAuthenticated:
		return pressure.NewOAuthMulti(pressure.OAuthConfig{
			AuthEndpoint:  values.AuthEndpoint,
			DataEndpoints: values.DataEndpoints,
			ClientID:      values.ClientID,
			ClientSecret:  values.ClientSecret,
		}, client)
	case config.PressureKindDisabled:
		return nil, nil
	default:
		return nil, fmt.Errorf("unknown pressure policy kind: %d", values.Kind)
	}
}
