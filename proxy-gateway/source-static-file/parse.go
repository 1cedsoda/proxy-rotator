package staticfile

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"proxy-gateway/core"
)

// LoadProxies reads a proxies file and parses each non-empty, non-comment line.
func LoadProxies(path string, format core.ProxyFormat) ([]core.SourceProxy, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	defer f.Close()

	var proxies []core.SourceProxy
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		p, err := core.ParseProxyLine(line, format)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: invalid proxy entry %q: %w", path, lineNum, line, err)
		}
		proxies = append(proxies, p)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return proxies, nil
}
