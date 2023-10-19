package loghttp

import (
	"net/http"
	"net/http/httputil"

	log "github.com/golang/glog"
	"github.com/google/uuid"
)

// Register wraps http.DefaultTransport with logging of the requests and responses.
func Register() {
	http.DefaultTransport = &glogTransport{base: http.DefaultTransport}
}

type glogTransport struct {
	base http.RoundTripper
}

func (g *glogTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	reqid := uuid.New().String()
	if reqStr, err := httputil.DumpRequest(req, true); err == nil {
		log.Infof("%v: HTTP Request: %v", reqid, string(reqStr))
	} else {
		log.Infof("%v: HTTP Request: %v", reqid, req)
	}
	resp, err := g.base.RoundTrip(req)
	if err != nil {
		log.Infof("%v: HTTP Error: %v", reqid, err)
		return nil, err
	}
	if respStr, err := httputil.DumpResponse(resp, true); err == nil {
		log.Infof("%v: HTTP Response: %v", reqid, string(respStr))
	} else {
		log.Infof("%v: HTTP Response: %v", reqid, resp)
	}
	return resp, nil
}
