package audit

import (
	"net/http"
	"net/http/httptest"
)

func newRequest(remoteAddr string, headers map[string]string) *http.Request {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = remoteAddr
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	return r
}
