// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/marmotdata/marmot/sdk/go/auth"
)

// newTestClient points an iamClient at a stub server.
func newTestClient(t *testing.T, handler http.HandlerFunc) *iamClient {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &iamClient{host: srv.URL, cred: auth.APIKey("test"), http: srv.Client()}
}

// alwaysRespond answers every request the same way.
func alwaysRespond(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

// readsEmptyPolicy serves GET so a test can concentrate on what writes do.
func readsEmptyPolicy(onWrite http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"etag":"e1","bindings":[]}`))
			return
		}
		onWrite(w, r)
	}
}

// Marmot answers 501; older builds fall through to a bare 404. Both mean the
// endpoints are absent, and reporting either as a plain "Not Found" reads as
// though the asset were missing.
func TestPolicyRequestsReportAMissingAPI(t *testing.T) {
	for _, status := range []int{http.StatusNotImplemented, http.StatusNotFound} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			c := newTestClient(t, alwaysRespond(status, `{"error":"Not found"}`))

			var unsupported *errNoPolicyAPI
			if _, err := c.GetPolicy(t.Context(), "asset", "a1"); !errors.As(err, &unsupported) {
				t.Fatalf("GetPolicy: got %v, want *errNoPolicyAPI", err)
			}
			if _, err := c.SetPolicy(t.Context(), "asset", "a1", iamPolicy{}); !errors.As(err, &unsupported) {
				t.Fatalf("SetPolicy: got %v, want *errNoPolicyAPI", err)
			}
			// The host and the status are both named: a configuration aimed at
			// the wrong instance is the other way to arrive here, and the code
			// tells a 501 from a proxy swallowing the route.
			for _, want := range []string{c.host, strconv.Itoa(status)} {
				if !strings.Contains(unsupported.Error(), want) {
					t.Errorf("error %q does not mention %q", unsupported.Error(), want)
				}
			}
		})
	}
}

// A stale etag is a conflict, not a missing API.
func TestSetPolicyReportsAConflict(t *testing.T) {
	c := newTestClient(t, alwaysRespond(http.StatusConflict, `{"error":"changed"}`))
	if _, err := c.SetPolicy(t.Context(), "asset", "a1", iamPolicy{}); !errors.Is(err, errPolicyConflict) {
		t.Fatalf("got %v, want errPolicyConflict", err)
	}
}

// A write that lands on the second attempt is the case the retry exists for.
func TestModifyPolicyRetriesUntilTheWriteLands(t *testing.T) {
	var writes int
	c := newTestClient(t, readsEmptyPolicy(func(w http.ResponseWriter, _ *http.Request) {
		if writes++; writes == 1 {
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":"changed"}`))
			return
		}
		_, _ = w.Write([]byte(`{"etag":"e2","bindings":[]}`))
	}))

	if err := c.modifyPolicy(t.Context(), "asset", "a1", func(*iamPolicy) {}); err != nil {
		t.Fatalf("modifyPolicy: %v", err)
	}
	if writes != 2 {
		t.Errorf("wrote %d times, want 2", writes)
	}
}

// Contention that never clears has to give up rather than spin, and the caller
// has to be able to tell why it stopped.
func TestModifyPolicyGivesUpOnEndlessConflicts(t *testing.T) {
	var writes int
	c := newTestClient(t, readsEmptyPolicy(func(w http.ResponseWriter, _ *http.Request) {
		writes++
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"changed"}`))
	}))

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()

	err := c.modifyPolicy(ctx, "asset", "a1", func(*iamPolicy) {})
	if !errors.Is(err, errPolicyConflict) && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v, want a conflict or a deadline", err)
	}
	if writes < 2 {
		t.Errorf("gave up after %d writes; the conflict was never retried", writes)
	}
}

// A missing API must not be mistaken for drift. Read used to match the word
// "Not Found" in the error text and drop the resource from state, which on an
// open-source instance silently discarded every access grant it managed.
func TestReadDoesNotTreatAMissingAPIAsDrift(t *testing.T) {
	c := newTestClient(t, alwaysRespond(http.StatusNotFound, `{"error":"Not found"}`))
	_, err := c.GetPolicy(t.Context(), "asset", "a1")

	var unsupported *errNoPolicyAPI
	if !errors.As(err, &unsupported) {
		t.Fatalf("got %v, want *errNoPolicyAPI", err)
	}
	if strings.Contains(err.Error(), "Not Found") {
		t.Error("the message still reads as a missing resource")
	}
}
