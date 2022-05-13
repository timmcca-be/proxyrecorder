// Package proxy contains the implementation of the proxy used to record
// traffic.
package proxy

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"

	"github.com/dnerdy/proxyrecorder/pkg/recorder"
)

type RequestInfo struct {
	RequestID        int           `json:"requestID"`
	OperationType    OperationType `json:"operationType"`
	OperationName    string        `json:"operationName"`
	WillSnapshot     bool          `json:"willSnapshot"`
	ShapshotComplete bool          `json:"snapshotComplete"`
}

type RequestSelector interface {
	ShouldRecordRequest(r GraphQLRequest) bool
	ShouldSnapshotRequest(r GraphQLRequest) bool
}

type Reporter interface {
	Report(label string, message string)
}

type Snapshotter interface {
	TakeSnapshot(r GraphQLRequest) ([]byte, error)
	SnapshotInfo() string
}

type Handler struct {
	snapshotter     Snapshotter
	recorder        recorder.RecorderSaver
	selector        RequestSelector
	reporter        Reporter
	requestInfoChan chan RequestInfo
	host            string
	proxyOrigin     *url.URL
	proxy           *httputil.ReverseProxy
	nextRequestID   int
	requestContent  map[*http.Request][]byte
	// Hold when updating nextRequestID or requestContent
	mu sync.Mutex
}

func NewHandler(
	host string,
	endpoint string,
	snapshotter Snapshotter,
	rec recorder.RecorderSaver,
	selector RequestSelector,
	reporter Reporter,
	requestInfoChan chan RequestInfo,
) (*Handler, error) {
	proxyOrigin, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}

	nextRequestID, err := rec.NextRequestID()
	if err != nil {
		return nil, err
	}

	handler := &Handler{
		snapshotter:     snapshotter,
		recorder:        rec,
		selector:        selector,
		reporter:        reporter,
		requestInfoChan: requestInfoChan,
		host:            host,
		proxyOrigin:     proxyOrigin,
		requestContent:  make(map[*http.Request][]byte),
		nextRequestID:   nextRequestID,
	}

	handler.proxy = httputil.NewSingleHostReverseProxy(proxyOrigin)
	handler.proxy.Director = handler.ProxyDirector
	handler.proxy.ModifyResponse = handler.ProxyResponseHandler

	if nextRequestID != 1 {
		handler.log("warning", fmt.Sprintf("existing requests in output dir, next request id: %d", nextRequestID))
	}

	return handler, nil
}

func (h *Handler) ProxyDirector(req *http.Request) {
	req.Host = h.host
	req.URL.Scheme = h.proxyOrigin.Scheme
	req.URL.Host = h.proxyOrigin.Host

	var content []byte

	if req.Body != nil {
		content, _ = ioutil.ReadAll(req.Body)
		req.Body.Close()
		req.Body = ioutil.NopCloser(bytes.NewReader(content))
	}

	h.mu.Lock()
	h.requestContent[req] = content
	h.mu.Unlock()
}

func (h *Handler) log(label, message string) {
	h.reporter.Report(label, message)
}

type GraphQLRequest struct {
	OperationName string                 `json:"operationName"`
	Variables     map[string]interface{} `json:"variables"`
	Query         string                 `json:"query"`

	// Set after the operation is parsed
	OperationType OperationType `json:"-"`
}

func ParseRequest(content []byte) (GraphQLRequest, error) {
	var graphQLRequest GraphQLRequest
	err := json.Unmarshal(content, &graphQLRequest)
	if err != nil {
		return GraphQLRequest{}, err
	}

	graphQLRequest.OperationType = OperationTypeUnknown

	switch {
	case strings.HasPrefix(graphQLRequest.Query, "query"):
		graphQLRequest.OperationType = OperationTypeQuery
	case strings.HasPrefix(graphQLRequest.Query, "mutation"):
		graphQLRequest.OperationType = OperationTypeMutation
	}

	return graphQLRequest, nil
}

type OperationType string

const (
	OperationTypeUnknown  OperationType = "unknown"
	OperationTypeQuery    OperationType = "query"
	OperationTypeMutation OperationType = "mutation"
)

func (h *Handler) ProxyResponseHandler(resp *http.Response) error {
	// Look for graphql requests.
	if !strings.Contains(resp.Request.URL.Path, "/backend-graphql/") {
		return nil
	}

	h.mu.Lock()
	requestContent := h.requestContent[resp.Request]
	delete(h.requestContent, resp.Request)
	h.mu.Unlock()

	if string(requestContent) == "" {
		h.log("warning", "no request content, "+resp.Request.URL.Path)
		return nil
	}

	graphQLRequest, err := ParseRequest(requestContent)

	if err != nil {
		h.reporter.Report("error", err.Error())
		return nil
	}

	if !h.selector.ShouldRecordRequest(graphQLRequest) {
		return nil
	}

	responseContent, err := readAndResetResponseContent(resp)
	if err != nil {
		h.log("error", "could not read response")
		return nil
	}

	h.mu.Lock()
	currentRequestID := h.nextRequestID
	h.nextRequestID += 1
	h.mu.Unlock()

	shouldSnapshot := h.selector.ShouldSnapshotRequest(graphQLRequest)

	// Send initial request info (may be updated below)
	h.requestInfoChan <- RequestInfo{
		RequestID:     currentRequestID,
		OperationType: graphQLRequest.OperationType,
		OperationName: graphQLRequest.OperationName,
		WillSnapshot:  shouldSnapshot,
	}

	h.recorder.SaveRequest(currentRequestID, requestContent)
	h.recorder.SaveResponse(currentRequestID, responseContent)

	h.log(
		string(graphQLRequest.OperationType),
		fmt.Sprintf(
			"%s %s",
			h.recorder.FormatRequestID(currentRequestID),
			graphQLRequest.OperationName,
		),
	)

	if shouldSnapshot {
		err := TakeSnapshot(
			currentRequestID,
			graphQLRequest,
			h.snapshotter,
			h.recorder,
			h.reporter,
		)

		// Update request info
		h.requestInfoChan <- RequestInfo{
			RequestID:        currentRequestID,
			OperationType:    graphQLRequest.OperationType,
			OperationName:    graphQLRequest.OperationName,
			WillSnapshot:     shouldSnapshot,
			ShapshotComplete: err == nil,
		}
	}

	return nil
}

func TakeSnapshot(
	requestID int,
	graphQLRequest GraphQLRequest,
	snapshotter Snapshotter,
	rec recorder.RecorderSaver,
	reporter Reporter,
) error {
	reporter.Report("", fmt.Sprintf("taking a snapshot, %s...", snapshotter.SnapshotInfo()))
	snapshot, err := snapshotter.TakeSnapshot(graphQLRequest)
	if err != nil {
		reporter.Report("error", err.Error())
		return err
	} else {
		reporter.Report("", "...done")
	}
	rec.SaveSnapshot(requestID, snapshot)
	return nil
}

func readAndResetResponseContent(resp *http.Response) ([]byte, error) {
	original, err := ioutil.ReadAll(resp.Body)

	err = resp.Body.Close()
	if err != nil {
		return nil, err
	}

	content := original

	if resp.Header.Get("content-encoding") == "gzip" {
		gzipReader, err := gzip.NewReader(bytes.NewReader(original))
		if err != nil {
			return nil, err
		}
		content, err = ioutil.ReadAll(gzipReader)
		if err != nil {
			return nil, err
		}
		gzipReader.Close()
	}

	body := ioutil.NopCloser(bytes.NewReader(original))
	resp.Body = body

	return content, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.proxy.ServeHTTP(w, r)
}
