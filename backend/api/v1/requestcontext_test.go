// api/v1/requestcontext_test.go

package v1_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/smegg99/s99ab/auth/kratosauth"

	apiv1 "github.com/smegg99/goptivum/backend/api/v1"
	"github.com/smegg99/goptivum/backend/api/v1/gen"
)

// A handler's context has to be its request's own. database/sql and sqlite
// read it from goroutines of their own that outlive the handler, and gin hands
// its pooled context to the next request as soon as the response is written.
//
// Only a real server shows it: httptest.NewRequest carries a context that can
// never be cancelled, and neither database/sql nor sqlite watches one. The failure
// is a data race, so this test fails under -race and passes silently without.
func TestConcurrentRequestsKeepTheirOwnContext(t *testing.T) {
	server, _ := adminServer(t, "")
	live := httptest.NewServer(server.Handler())
	t.Cleanup(live.Close)

	const workers, rounds = 8, 25
	failures := make(chan error, workers*rounds)
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for range rounds {
				request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, live.URL+"/api/v1/plans", nil)
				if err != nil {
					failures <- err
					return
				}
				request.Header.Set("Authorization", asPlanner)
				response, err := live.Client().Do(request)
				if err != nil {
					failures <- err
					return
				}
				response.Body.Close()
				if response.StatusCode != http.StatusOK {
					failures <- fmt.Errorf("list plans = %d, want 200", response.StatusCode)
				}
			}
		})
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
}

// capture records the context ListPlans is handed. Nothing else is called.
type capture struct {
	gen.StrictServerInterface
	got chan context.Context
}

func (c capture) ListPlans(ctx context.Context, _ gen.ListPlansRequestObject) (gen.ListPlansResponseObject, error) {
	c.got <- ctx
	return gen.ListPlans200JSONResponse{Items: []gen.Plan{}}, nil
}

// The same boundary without the race detector, which the ordinary test run
// does not use.
func TestAHandlerIsNotHandedThePooledGinContext(t *testing.T) {
	handlers := capture{got: make(chan context.Context, 1)}
	server, err := apiv1.New(apiv1.Options{Handlers: handlers, Kratos: adminFake(t)})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/plans", nil)
	request.Header.Set("Authorization", asPlanner)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("list plans = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}

	ctx := <-handlers.got
	if _, pooled := ctx.(*gin.Context); pooled {
		t.Fatal("ListPlans was handed the *gin.Context, which gin reuses for the next request")
	}
	if _, ok := kratosauth.FromContext(ctx); !ok {
		t.Error("the handler's context lost the identity the session middleware resolved")
	}
}
