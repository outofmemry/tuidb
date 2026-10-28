package web

import (
	"regexp"
	"strconv"
)

var (
	supportedRe = regexp.MustCompile(`(?s)SUPPORTED\s*=\s*new Set\(\[(.*?)\]\)`)
	quotedRe    = regexp.MustCompile(`'([a-z0-9_:]+)'`)
	arrayFromRe = regexp.MustCompile("Array\\.from\\(\\{\\s*length:\\s*(\\d+)\\s*\\},\\s*\\(_,\\s*i\\)\\s*=>\\s*`([a-z0-9_]*)\\$\\{i\\s*\\+\\s*1\\}`\\)")
)

// SupportedActions parses the SUPPORTED set out of the source of web/src/routes/Workspace.tsx: the quoted
// action names, plus the generated ones (`...Array.from({ length: 9 }, (_, i) => `tab_${i + 1}`)` gives
// tab_1..tab_9). The M14 parity checks compare it with the keymap, so the list lives in one place.
func SupportedActions(workspaceTSX []byte) []string {
	m := supportedRe.FindSubmatch(workspaceTSX)
	if m == nil {
		return nil
	}
	body := string(m[1])
	var out []string
	for _, a := range arrayFromRe.FindAllStringSubmatch(body, -1) {
		n, _ := strconv.Atoi(a[1])
		for i := 1; i <= n; i++ {
			out = append(out, a[2]+strconv.Itoa(i))
		}
	}
	body = arrayFromRe.ReplaceAllString(body, "")
	for _, q := range quotedRe.FindAllStringSubmatch(body, -1) {
		out = append(out, q[1])
	}
	seen := map[string]bool{}
	uniq := out[:0]
	for _, a := range out {
		if !seen[a] {
			seen[a] = true
			uniq = append(uniq, a)
		}
	}
	return uniq
}
