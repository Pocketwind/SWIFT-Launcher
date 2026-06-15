package useragent

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	appNamePattern    = regexp.MustCompile(`^[a-zA-Z0-9]{1,30}$`)
	appVersionPattern = regexp.MustCompile(`^[\d._-]{1,8}$`)
	bic8Pattern       = regexp.MustCompile(`^[A-Z]{4}[A-Z]{2}[A-Z0-9]{2}$`)
	leiPattern        = regexp.MustCompile(`^[A-Z0-9]{4}00[A-Z0-9]{12}\d{2}$`)
	vatPattern        = regexp.MustCompile(`^[A-Za-z0-9/\-?:().,'+ ]{1,35}$`)
	hashPattern       = regexp.MustCompile(`^[a-fA-F0-9]{64}$`)
)

// User-Agent Maker
func BuildUserAgent(config UserAgentConfig, existing string) (string, error) {
	if !appNamePattern.MatchString(config.AppName) {
		return "", fmt.Errorf("invalid AppName")
	}
	if !appVersionPattern.MatchString(config.AppVersion) {
		return "", fmt.Errorf("invalid AppVersion")
	}

	var ext strings.Builder
	fmt.Fprintf(&ext, " A/%s/%s", config.AppName, config.AppVersion)

	if config.PartnerBIC != "" {
		if !bic8Pattern.MatchString(config.PartnerBIC) {
			return "", fmt.Errorf("invalid PartnerBIC8")
		}
		fmt.Fprintf(&ext, " P/%s", config.PartnerBIC)
	}

	id := config.CustomerIdentifier
	var idStr string
	switch strings.ToUpper(id.Type) {
	case "BIC":
		if !bic8Pattern.MatchString(id.Value) {
			return "", fmt.Errorf("invalid CustomerBIC8")
		}
		idStr = fmt.Sprintf("B/%s", id.Value)
	case "LEI":
		if !leiPattern.MatchString(id.Value) {
			return "", fmt.Errorf("invalid LEI")
		}
		idStr = fmt.Sprintf("L/%s", id.Value)
	default:
		return "", fmt.Errorf("unsupported customer ID type")
	}

	fmt.Fprintf(&ext, " %s", idStr)
	return existing + ext.String(), nil
}
