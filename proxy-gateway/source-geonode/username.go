package geonode

import (
	"fmt"
	"strings"

	"proxy-gateway/core"
)

// BuildUsername builds the upstream proxy username for a single request.
func BuildUsername(cfg *Config) string {
	country := pickCountry(cfg.Countries)

	if cfg.Session.Type == SessionTypeSticky {
		return buildSticky(cfg.Username, cfg.Session.SessTime, randomSessionID(), country)
	}
	return buildRotating(cfg.Username, country)
}

// RotateUsername rebuilds the username for force-rotation (generates a new session ID).
func RotateUsername(cfg *Config) string {
	return BuildUsername(cfg)
}

func buildRotating(username string, country core.Country) string {
	if country == "" {
		return username
	}
	return fmt.Sprintf("%s-country-%s", username, strings.ToUpper(country.AsParamStr()))
}

func buildSticky(username string, sessTime uint32, sessionID string, country core.Country) string {
	parts := []string{
		username,
		fmt.Sprintf("session-%s", sessionID),
		fmt.Sprintf("sessTime-%d", sessTime),
	}
	if country != "" {
		parts = append(parts, fmt.Sprintf("country-%s", strings.ToUpper(country.AsParamStr())))
	}
	return strings.Join(parts, "-")
}

func pickCountry(countries []core.Country) core.Country {
	if len(countries) == 0 {
		return ""
	}
	return countries[int(core.CheapRandom())%len(countries)]
}

func randomSessionID() string {
	a := core.CheapRandom()
	b := core.CheapRandom()
	return fmt.Sprintf("%016x", a^(b<<32))
}
