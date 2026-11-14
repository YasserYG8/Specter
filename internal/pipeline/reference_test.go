package pipeline

import "strings"

// The reference functions hold the bodies of StripANSI, SanitizeTerminalOutput and HasANSI as they were before the
// escape byte check was added: the bare regular expression calls. The fuzz tests compare the real functions with
// them, so a shortcut that changes any result fails at once.

func refStripANSI(s string) string {
	return anyANSIRegex.ReplaceAllString(s, "")
}

func refSanitize(s string) string {
	return nonSGRControlRegex.ReplaceAllString(s, "")
}

func refHasANSI(s string) bool {
	return ansiSGRRegex.MatchString(s) || anyANSIRegex.MatchString(s)
}

// refIsRecordStart is isRecordStart as it was before the JSON decision was kept in a memo: it calls ParseJSONLog
// itself. The render fuzz test uses it as an independent reference for the grouper's record decision.
func refIsRecordStart(clean string) (bool, Severity, bool, bool) {
	if clean == "" {
		return false, SeverityNone, false, false
	}
	if pythonTraceHeaderRegex.MatchString(clean) {
		return true, SeverityError, true, false
	}
	if goPanicHeaderRegex.MatchString(clean) {
		return true, SeverityError, true, strings.Contains(clean, "goroutine")
	}
	if ParseJSONLog(clean) != nil {
		return true, DetectSeverity(clean), false, false
	}
	if dotnetLevelRegex.MatchString(clean) {
		return true, DetectSeverity(clean), false, false
	}
	if bracketedLevelRegex.MatchString(clean) || monologLevelRegex.MatchString(clean) || columnarLevelRegex.MatchString(clean) {
		return true, DetectSeverity(clean), false, false
	}
	if _, isHTTP := DetectHTTPStatus(clean); isHTTP {
		return true, DetectSeverity(clean), false, false
	}
	if positionalWordRegex.MatchString(clean) || cookbookLevelRegex.MatchString(clean) || celeryLevelRegex.MatchString(clean) {
		return true, DetectSeverity(clean), false, false
	}
	if !strings.HasPrefix(clean, " ") && !strings.HasPrefix(clean, "\t") {
		cleanLower := strings.ToLower(clean)
		if strings.Contains(cleanLower, "exception:") || strings.Contains(cleanLower, "error:") || strings.Contains(cleanLower, "fatal error:") {
			return true, SeverityError, true, false
		}
	}
	return false, SeverityNone, false, false
}
