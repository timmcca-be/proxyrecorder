package proxy

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"
)

type requestRecord struct {
	recordType string
	requestID  int
	content    []byte
}

type testRequestRecorder struct {
	records []requestRecord
}

func (r *testRequestRecorder) SaveRequest(requestID int, content []byte) error {
	r.records = append(r.records, requestRecord{"request", requestID, content})
	return nil
}

func (r *testRequestRecorder) SaveResponse(requestID int, content []byte) error {
	r.records = append(r.records, requestRecord{"response", requestID, content})
	return nil
}

func (r *testRequestRecorder) SaveSnapshot(requestID int, content []byte) error {
	r.records = append(r.records, requestRecord{"snapshot", requestID, content})
	return nil
}

func (r *testRequestRecorder) FormatRequestID(requestID int) string {
	return fmt.Sprintf("%06d", requestID)
}

func (r *testRequestRecorder) NextRequestID() (int, error) {
	return 1, nil
}

type testRequestSelector struct{}

func (s *testRequestSelector) ShouldRecordRequest(r GraphQLRequest) bool {
	return r.OperationName == "operationToRecord"
}

func (s *testRequestSelector) ShouldSnapshotRequest(r GraphQLRequest) bool {
	return r.OperationType == OperationTypeMutation
}

type testReport struct {
	label   string
	message string
}

type testReporter struct {
	reports []testReport
}

func (r *testReporter) Report(label string, message string) {
	r.reports = append(r.reports, testReport{label, message})
}

type testSnapshotter struct {
	snapshotContent string
	snapshotError   error
	snapshotInfo    string
}

func (s *testSnapshotter) TakeSnapshot(_ GraphQLRequest) ([]byte, error) {
	return []byte(s.snapshotContent), s.snapshotError
}

func (s *testSnapshotter) SnapshotInfo() string {
	return s.snapshotInfo
}

type staticHandler struct {
	Content string
}

func (h *staticHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte(h.Content))
}

type handlerSuite struct {
	suite.Suite
	snapshotter     *testSnapshotter
	requestRecorder *testRequestRecorder
	requestSelector *testRequestSelector
	reporter        *testReporter
	proxyRecorder   *Handler
	origin          *staticHandler
	server          *httptest.Server
	requestInfoChan chan RequestInfo
}

func (suite *handlerSuite) BeforeTest(suiteName, testName string) {
	suite.snapshotter = &testSnapshotter{}
	suite.requestRecorder = &testRequestRecorder{}
	suite.requestSelector = &testRequestSelector{}
	suite.reporter = &testReporter{}
	suite.origin = &staticHandler{}
	suite.server = httptest.NewServer(suite.origin)
	suite.requestInfoChan = make(chan RequestInfo, 100)
	var err error
	suite.proxyRecorder, err = NewHandler(
		"https://en.khanacademy.org",
		suite.server.URL,
		suite.snapshotter,
		suite.requestRecorder,
		suite.requestSelector,
		suite.reporter,
		suite.requestInfoChan,
	)
	suite.Require().NoError(err)
}

func (suite *handlerSuite) AfterTest(suiteName, testName string) {
	suite.server.Close()
}

func (suite *handlerSuite) TestAllURLsAreProxied() {
	req := httptest.NewRequest("GET", "http://www.khanacademy.org/some/endpoint", nil)
	w := httptest.NewRecorder()

	suite.origin.Content = "some content from the origin"
	suite.proxyRecorder.ServeHTTP(w, req)

	resp := w.Result()
	body, _ := ioutil.ReadAll(resp.Body)

	suite.Assert().Equal("some content from the origin", string(body))
	suite.Assert().Len(suite.reporter.reports, 0)
}

func (suite *handlerSuite) TestNonSelectedRequestsAreSkipped() {
	req := httptest.NewRequest("GET", "http://www.khanacademy.org/api/internal/graphql", strings.NewReader(
		`{"operationName": "someOperation", "query": "query someOperation { field }"}`,
	))
	w := httptest.NewRecorder()

	suite.proxyRecorder.ServeHTTP(w, req)

	suite.Require().Len(suite.reporter.reports, 0)
}

func (suite *handlerSuite) TestSelectedRequestsAreRecorded() {
	req := httptest.NewRequest("GET", "http://www.khanacademy.org/api/internal/graphql", strings.NewReader(
		`{"operationName": "operationToRecord", "query": "query operationToRecord { someQuery }"}`,
	))
	w := httptest.NewRecorder()
	suite.proxyRecorder.ServeHTTP(w, req)
	suite.Require().Equal(
		[]testReport{
			{"query", "000001 operationToRecord"},
		},
		suite.reporter.reports,
	)

	// Perform another non-GTP operation. The operation shouldn't be recorded.
	req = httptest.NewRequest("GET", "http://www.khanacademy.org/api/internal/graphql", strings.NewReader(
		`{"operationName": "someOperation", "query": "query someOperation { field }"}`,
	))
	w = httptest.NewRecorder()
	suite.proxyRecorder.ServeHTTP(w, req)
	suite.Require().Len(suite.reporter.reports, 1)

	// Perform another GTP operation. The request number should now be 2.
	req = httptest.NewRequest("GET", "http://www.khanacademy.org/api/internal/graphql", strings.NewReader(
		`{"operationName": "operationToRecord", "query": "query operationToRecord { someQuery }"}`,
	))
	w = httptest.NewRecorder()
	suite.proxyRecorder.ServeHTTP(w, req)
	suite.Require().Equal(
		[]testReport{
			{"query", "000001 operationToRecord"},
			{"query", "000002 operationToRecord"},
		},
		suite.reporter.reports,
	)
}

func (suite *handlerSuite) TestSelectedRequestsAreSnapshotted() {
	req := httptest.NewRequest("GET", "http://www.khanacademy.org/api/internal/graphql", strings.NewReader(
		`{"operationName": "operationToRecord", "query": "mutation operationToRecord { someMutation }"}`,
	))
	w := httptest.NewRecorder()

	suite.origin.Content = "some content from the origin"
	suite.snapshotter.snapshotContent = `{"test": "snapshot"}`
	suite.snapshotter.snapshotInfo = `(snapshot description)`

	suite.proxyRecorder.ServeHTTP(w, req)

	suite.Require().Equal(
		[]testReport{
			{"mutation", "000001 operationToRecord"},
			{"", "taking a snapshot, (snapshot description)..."},
			{"", "...done"},
		},
		suite.reporter.reports,
	)
}

func (suite *handlerSuite) TestDataIsRecorded() {
	req := httptest.NewRequest("GET", "http://www.khanacademy.org/api/internal/graphql", strings.NewReader(
		`{"operationName": "operationToRecord", "query": "mutation operationToRecord { someMutation }"}`,
	))
	w := httptest.NewRecorder()

	suite.origin.Content = "some content from the origin"
	suite.snapshotter.snapshotContent = `{"test": "snapshot"}`

	suite.proxyRecorder.ServeHTTP(w, req)

	suite.Require().Equal(
		[]requestRecord{
			{
				recordType: "request",
				requestID:  1,
				content:    []byte(`{"operationName": "operationToRecord", "query": "mutation operationToRecord { someMutation }"}`),
			},
			{
				recordType: "response",
				requestID:  1,
				content:    []byte(`some content from the origin`),
			},
			{
				recordType: "snapshot",
				requestID:  1,
				content:    []byte(`{"test": "snapshot"}`),
			},
		},
		suite.requestRecorder.records,
	)
}

func TestHandler(t *testing.T) {
	suite.Run(t, new(handlerSuite))
}
