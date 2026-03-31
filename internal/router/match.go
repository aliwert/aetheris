package router

import (
	"net/http"
	"strings"

	api "github.com/aliwert/aetheris/pkg/aetherisapi"
)

// the internal representation of a RouteConfig after
// all string parsing and normalisation has been performed, pre-computing
// these values at reload time keeps the hot path (Match) allocation-free
type compiledRoute struct {
	id              string
	pathPrefix      string
	methods         map[string]struct{}
	requiredHeaders map[string]string
	priority        int
	original        api.RouteConfig
}

// converts a RouteConfig into a compiledRoute
func compile(rc api.RouteConfig) compiledRoute {
	cr := compiledRoute{
		id:         rc.ID,
		pathPrefix: normalisePath(rc.PathPrefix),
		original:   rc,
	}

	if len(rc.Methods) > 0 {
		cr.methods = make(map[string]struct{}, len(rc.Methods))
		for _, m := range rc.Methods {
			cr.methods[strings.ToUpper(m)] = struct{}{}
		}
	}

	if len(rc.Headers) > 0 {
		cr.requiredHeaders = make(map[string]string, len(rc.Headers))
		for k, v := range rc.Headers {
			cr.requiredHeaders[http.CanonicalHeaderKey(k)] = v
		}
	}

	cr.priority = len(cr.pathPrefix) * 10
	if cr.methods != nil {
		cr.priority += 5
	}
	cr.priority += len(cr.requiredHeaders) * 3

	return cr
}

func (cr *compiledRoute) matches(r *http.Request) bool {
	if cr.pathPrefix != "" &&
		!strings.HasPrefix(r.URL.Path, cr.pathPrefix) {
		return false
	}

	if cr.methods != nil {
		if _, ok := cr.methods[r.Method]; !ok {
			return false
		}
	}

	for name, wantVal := range cr.requiredHeaders {
		if r.Header.Get(name) != wantVal {
			return false
		}
	}

	return true
}

func normalisePath(p string) string {
	if p == "" {
		return ""
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	if p != "/" {
		p = strings.TrimRight(p, "/")
	}
	return p
}
