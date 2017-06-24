package librato

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

var (
	doesntmatter = time.Hour
)

func ExampleNew() {
	start := time.Now()
	u, err := url.Parse(DefaultURL)
	if err != nil {
		log.Fatal(err)
	}
	u.User = url.UserPassword("libratoUser", "libratoPassword/Token")

	errHandler := func(err error) {
		log.Println(err)
	}
	p := New(u, 20*time.Second, WithErrorHandler(errHandler))
	c := p.NewCounter("i.am.a.counter")
	h := p.NewHistogram("i.am.a.histogram", DefaultBucketCount)
	g := p.NewGauge("i.am.a.gauge")

	// Pretend applicaion logic....
	c.Add(1)
	h.Observe(time.Now().Sub(start).Seconds()) // how long did it take the program to get here.
	g.Set(1000)
	// /Pretend

	// block until we report one final time
	p.Stop()
}

func TestLibratoReportRequestDebugging(t *testing.T) {
	for _, debug := range []bool{true, false} {
		t.Run(fmt.Sprintf("%t", debug), func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
			}))
			defer srv.Close()
			u, err := url.Parse(srv.URL)
			if err != nil {
				t.Fatal(err)
			}
			p := New(u, doesntmatter, func(p *Provider) { p.requestDebugging = debug })
			p.Stop()
			p.NewCounter("foo").Add(1) // need at least one metric in order to report
			err = p.(*Provider).report(u, doesntmatter)
			if err == nil {
				t.Fatal("expected error, got nil")
			}

			e, ok := err.(Error)
			if !ok {
				t.Fatalf("expected an Error, got %T: %q", err, err.Error())
			}

			req := e.Request()
			if debug {
				if req.URL.String() != u.String() {
					t.Fatalf("expected %s, got %s", u.String(), req.URL.String())
				}
				eContentType := "application/json"
				if v := req.Header.Get("Content-Type"); v != eContentType {
					t.Fatalf("expected %q, got %q", eContentType, v)
				}
				buf, _ := ioutil.ReadAll(req.Body)

				var payload map[string]interface{}
				if err := json.Unmarshal(buf, &payload); err != nil {
					t.Fatal("unexpected error", err)
				}
				if len(payload) == 0 {
					t.Fatal("expected payload with data, got empty payload")
				}
			} else {
				if req != nil {
					t.Errorf("expected no request, got %#v", req)
				}
			}

		})
	}
}

func TestLibratoRetriesWithErrors(t *testing.T) {
	var retried int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		retried++
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}

	var totalErrors, temporaryErrors, finalErrors int
	expectedRetries := 3
	errHandler := func(err error) {
		totalErrors++
		type temporary interface {
			Temporary() bool
		}
		if terr, ok := err.(temporary); ok {
			if terr.Temporary() {
				temporaryErrors++
			} else {
				finalErrors++
			}
		}
	}
	p := New(u, doesntmatter, WithErrorHandler(errHandler), WithRetries(expectedRetries), WithRequestDebugging()).(*Provider)
	p.Stop()
	p.NewCounter("foo").Add(1) // need at least one metric in order to report
	p.reportWithRetry(u, doesntmatter)

	if totalErrors != expectedRetries {
		t.Errorf("expected %d total errors, got %d", expectedRetries, totalErrors)
	}

	expectedTemporaryErrors := expectedRetries - 1
	if temporaryErrors != expectedTemporaryErrors {
		t.Errorf("expected %d temporary errors, got %d", expectedTemporaryErrors, temporaryErrors)
	}

	expectedFinalErrors := 1
	if finalErrors != expectedFinalErrors {
		t.Errorf("expected %d final errors, got %d", expectedFinalErrors, finalErrors)
	}

	if retried != expectedRetries {
		t.Errorf("expected %d retries, got %d", expectedRetries, retried)
	}
}

func TestLibratoSingleReport(t *testing.T) {
	user := os.Getenv("LIBRATO_TEST_USER")
	pwd := os.Getenv("LIBRATO_TEST_PWD")
	if user == "" || pwd == "" {
		t.Skip("LIBRATO_TEST_USER || LIBRATO_TEST_PWD unset")
	}
	rand.Seed(time.Now().UnixNano())
	u, err := url.Parse(DefaultURL)
	if err != nil {
		t.Fatalf("expected nil, got %q", err)
	}
	u.User = url.UserPassword(user, pwd)

	var p Provider
	p.source = "test.source"
	c := p.NewCounter("test.counter")
	g := p.NewGauge("test.gauge")
	h := p.NewHistogram("test.histogram", DefaultBucketCount)
	c.Add(float64(time.Now().Unix())) // increasing value
	g.Set(rand.Float64())
	h.Observe(10)
	h.Observe(100)
	h.Observe(150)

	// Call the reporter explicitly
	if err := p.report(u, 10*time.Second); err != nil {
		t.Fatalf("expected nil, got %q", err)
	}
}

func TestLibratoReport(t *testing.T) {
	user := os.Getenv("LIBRATO_TEST_USER")
	pwd := os.Getenv("LIBRATO_TEST_PWD")
	if user == "" || pwd == "" {
		t.Skip("LIBRATO_TEST_USER || LIBRATO_TEST_PWD unset")
	}
	rand.Seed(time.Now().UnixNano())
	u, err := url.Parse(DefaultURL)
	if err != nil {
		t.Fatalf("expected nil, got %q", err)
	}
	//u.Host = "asdasda"
	u.User = url.UserPassword(user, pwd)

	errHandler := func(err error) {
		t.Errorf("expected nil, got %q", err)
	}

	p := New(u, time.Second, WithSource("test.source"), WithErrorHandler(errHandler))
	c := p.NewCounter("test.counter")
	g := p.NewGauge("test.gauge")
	h := p.NewHistogram("test.histogram", DefaultBucketCount)

	done := make(chan struct{})

	go func() {
		for i := 0; i < 30; i++ {
			c.Add(float64(time.Now().Unix())) // increasing value
			g.Set(rand.Float64())
			h.Observe(rand.Float64() * 100)
			h.Observe(rand.Float64() * 100)
			h.Observe(rand.Float64() * 100)
			time.Sleep(100 * time.Millisecond)
		}
		p.Stop()
		close(done)
	}()

	<-done
}

func TestLibratoHistogramJSONMarshalers(t *testing.T) {
	h := Histogram{name: "test.histogram", buckets: DefaultBucketCount, percentilePrefix: ".p"}
	h.reset()
	h.Observe(10)
	h.Observe(100)
	h.Observe(150)
	ePeriod := 1.0
	d := h.measures(ePeriod)
	if len(d) != 4 {
		t.Fatalf("expected length of parts to be 4, got %d", len(d))
	}

	p1, err := json.Marshal(d[0])
	if err != nil {
		t.Fatal("unexpected error unmarshaling", err)
	}
	p99, err := json.Marshal(d[1])
	if err != nil {
		t.Fatal("unexpected error unmarshaling", err)
	}
	p95, err := json.Marshal(d[2])
	if err != nil {
		t.Fatal("unexpected error unmarshaling", err)
	}
	p50, err := json.Marshal(d[3])
	if err != nil {
		t.Fatal("unexpected error unmarshaling", err)
	}

	cases := []struct {
		eRaw, eName              string
		eCount                   int64
		eMin, eMax, eSum, eSumSq float64
		input                    []byte
	}{
		{
			eRaw:   `{"name":"test.histogram","period":1,"count":3,"sum":260,"min":10,"max":150,"sum_squares":32600}`,
			eName:  "test.histogram",
			eCount: 3, eMin: 10, eMax: 150, eSum: 260, eSumSq: 32600,
			input: p1,
		},
		{
			eRaw:   `{"name":"test.histogram.p99","period":1,"count":1,"sum":150,"min":150,"max":150,"sum_squares":22500}`,
			eName:  "test.histogram.p99",
			eCount: 1, eMin: 150, eMax: 150, eSum: 150, eSumSq: 22500,
			input: p99,
		},
		{
			eRaw:   `{"name":"test.histogram.p95","period":1,"count":1,"sum":150,"min":150,"max":150,"sum_squares":22500}`,
			eName:  "test.histogram.p95",
			eCount: 1, eMin: 150, eMax: 150, eSum: 150, eSumSq: 22500,
			input: p95,
		},
		{
			eRaw:   `{"name":"test.histogram.p50","period":1,"count":1,"sum":100,"min":100,"max":100,"sum_squares":10000}`,
			eName:  "test.histogram.p50",
			eCount: 1, eMin: 100, eMax: 100, eSum: 100, eSumSq: 10000,
			input: p50,
		},
	}

	for _, tc := range cases {
		t.Run(tc.eName, func(t *testing.T) {
			t.Parallel()
			if string(tc.input) != tc.eRaw {
				t.Errorf("expected %q\ngot %q", tc.eRaw, tc.input)
			}

			var tg gauge
			err := json.Unmarshal(tc.input, &tg)
			if err != nil {
				t.Fatal("unexpected error unmarshalling", err)
			}

			if tg.Name != tc.eName {
				t.Errorf("expected %q, got %q", tc.eName, tg.Name)
			}
			if tg.Count != tc.eCount {
				t.Errorf("expected %d, got %d", tc.eCount, tg.Count)
			}
			if tg.Period != ePeriod {
				t.Errorf("expected %f, got %f", ePeriod, tg.Period)
			}
			if math.Float64bits(tg.Sum) != math.Float64bits(tc.eSum) {
				t.Errorf("expected %f, got %f", tc.eSum, tg.Sum)
			}
			if math.Float64bits(tg.Min) != math.Float64bits(tc.eMin) {
				t.Errorf("expected %f, got %f", tc.eMin, tg.Min)
			}
			if math.Float64bits(tg.Max) != math.Float64bits(tc.eMax) {
				t.Errorf("expected %f, got %f", tc.eMin, tg.Max)
			}
			if math.Float64bits(tg.SumSq) != math.Float64bits(tc.eSumSq) {
				t.Errorf("expected %f, got %f", tc.eSumSq, tg.SumSq)
			}

		})
	}
}

func TestScrubbing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	errors := make([]error, 0, 100)
	var errCnt int
	errHandler := func(err error) {
		errors = append(errors, err)
		errCnt++
	}
	u.User = url.UserPassword("foo", "bar") // put user info into the URL
	p := New(u, doesntmatter, WithErrorHandler(errHandler), WithRequestDebugging())
	foo := p.NewCounter("foo")
	foo.Add(1)
	p.(*Provider).reportWithRetry(u, doesntmatter)

	for _, err := range errors {
		e, ok := err.(Error)
		if !ok {
			t.Fatalf("expected Error, got %T: %q", err, err.Error())
		}
		request := e.Request()
		if ahv := request.Header.Get("Authorization"); strings.Contains(ahv, "foo") {
			t.Error("expected Authorizaton header to not contain username, got:", ahv)
		}
		if request.URL.User != nil {
			t.Error("expected the request URL user to be nil, got", request.URL.User)
		}
	}

	// Close the server now so we get an error from the http client
	srv.Close()
	errors = errors[errCnt:]
	p.(*Provider).reportWithRetry(u, doesntmatter)

	for _, err := range errors {
		_, ok := err.(Error)
		if ok {
			t.Errorf("unexpected Error, got %T: %q", err, err.Error())
		}
		if es := err.Error(); strings.Contains(es, "foo") {
			t.Error("expected the error to not contain sensitive data, got", es)
		}
	}

	if errCnt != 2*DefaultNumRetries {
		t.Errorf("expected total error count to be %d, got %d", 2*DefaultNumRetries, errCnt)
	}

}

func TestWithResetCounters(t *testing.T) {
	for _, reset := range []bool{true, false} {
		t.Run(fmt.Sprintf("%t", reset), func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			defer srv.Close()
			u, err := url.Parse(srv.URL)
			if err != nil {
				t.Fatal(err)
			}
			p := New(u, doesntmatter, func(p *Provider) { p.resetCounters = reset })
			p.Stop()
			foo := p.NewCounter("foo")
			foo.Add(1)
			p.(*Provider).report(u, doesntmatter)

			var expected float64
			if reset {
				expected = 0
			} else {
				expected = 1
			}
			type valuer interface {
				Value() float64
			}
			if v := foo.(valuer).Value(); v != expected {
				t.Errorf("expected %f, got %f", expected, v)
			}
		})
	}
}
