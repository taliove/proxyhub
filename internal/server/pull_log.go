package server

import (
	"net/http"

	"github.com/taliove/proxyhub/internal/store"
)

// recordPullStatus leaves a pull_logs trace for one /sub request outcome
// (pull-guard ticket 01). Every exit of handleSubscription that the client can
// reach with a well-formed request goes through here, so a blocked pull is as
// visible in the log as a delivered one - the external answer stays a uniform
// 404 and only the recorded status tells them apart.
//
// endpointID is 0 when the /sub path itself is unknown: there is no endpoint row
// to attribute the attempt to, so the trace lands in the global (unowned) bucket.
// Persisting the trace must never break the response, so a write failure is only
// warned about, matching the pre-ticket behaviour of the success path.
//
// Not a route for the subscription test (ADR 0028 decision 1): that path runs the
// internal generation chain and deliberately never reaches this function.
func (s *Server) recordPullStatus(r *http.Request, endpointID int64, status string) {
	if err := s.st.RecordPull(store.PullRecord{
		EndpointID: endpointID,
		IP:         s.clientIP(r),
		UserAgent:  r.Header.Get("User-Agent"),
		Status:     status,
	}); err != nil {
		s.logger.Warn("record pull failed",
			"status", status, "endpoint_id", endpointID, "error", err)
	}
}
