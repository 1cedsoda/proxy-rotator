package utils

import (
	"fmt"

	"proxy-gateway/core"
)

const hexCharset = "0123456789abcdef"

// pickCountry selects a country from the list.
// With a seed: deterministic. Without (nil): random.
func pickCountry(countries []Country, seed *core.SessionSeed) Country {
	if len(countries) == 0 {
		return ""
	}
	if seed != nil {
		return countries[seed.Pick(len(countries))]
	}
	return countries[CheapRandom()%uint64(len(countries))]
}

// deriveSessionID returns a 16-char hex session identifier.
// With a seed: deterministic. Without (nil): random.
func deriveSessionID(seed *core.SessionSeed) string {
	if seed != nil {
		return seed.DeriveStringKey(hexCharset, 16)
	}
	return fmt.Sprintf("%016x", CheapRandom())
}
