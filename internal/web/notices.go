package web

import (
	"net/http"
	"regexp"
	"strings"
)

var noticeRe = regexp.MustCompile(`^(DEBUG[1-5]?|LOG|INFO|NOTICE|WARNING|EXCEPTION): (.*)$`)

type notice struct {
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

// noticeLines picks the server notices out of a result message: the postgres driver appends
// "SEVERITY: text" lines to the command tag, and a line that is not one continues the previous notice.
func noticeLines(msg string) (tag string, ns []notice) {
	var tagLines []string
	for _, l := range strings.Split(msg, "\n") {
		if m := noticeRe.FindStringSubmatch(l); m != nil {
			ns = append(ns, notice{m[1], m[2]})
		} else if len(ns) > 0 {
			ns[len(ns)-1].Message += "\n" + l
		} else {
			tagLines = append(tagLines, l)
		}
	}
	return strings.TrimSpace(strings.Join(tagLines, "\n")), ns
}

func (s *Server) notices(ss *session, r *http.Request) (any, error) {
	ss.mu.Lock()
	msg, ok := ss.notices[r.PathValue("rid")]
	ss.mu.Unlock()
	if !ok {
		return nil, bad("no notices for that result (none were raised, or it was evicted)")
	}
	tag, ns := noticeLines(msg)
	return map[string]any{"message": tag, "notices": ns}, nil
}
