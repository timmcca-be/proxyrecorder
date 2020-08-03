// Package tool contains the various handlers needed to support the proxy
// recorder frontend interface. The tool interface is powered by web sockets
// and a single REST API. When a request is recorded by the proxy, RequestInfo
// is sent to the tool over a channel. The tool forwards this info to the
// interface via a web socket. When the user wants to load the full data for a
// request, the interface makes a REST call to retrieve a full request Record.
package tool

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"sync"

	"github.com/dnerdy/proxyrecorder/pkg/proxy"
	"github.com/dnerdy/proxyrecorder/pkg/recorder"
	"github.com/gorilla/websocket"
)

type Record struct {
	RequestID       int                 `json:"requestID"`
	OperationType   proxy.OperationType `json:"operationType"`
	OperationName   string              `json:"operationName"`
	Request         string              `json:"request"`
	Response        string              `json:"response"`
	CurrentSnapshot string              `json:"currentSnapshot"`
	PriorSnapshot   string              `json:"priorSnapshot"`
}

type Handler struct {
	recorder    *recorder.Recorder
	mux         http.Handler
	connections map[*websocket.Conn]struct{}
	connMu      sync.Mutex
	upgrader    websocket.Upgrader
}

type WebsocketMessage struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

// NewHandlerAndStartWebsocketWorker creates a handler with all of the tool
// routes. It also creates a goroutine that forwards RequestInfo to all
// connected web socket clients.
func NewHandlerAndStartWebsocketWorker(
	rec *recorder.Recorder,
	requestInfoChan chan proxy.RequestInfo,
) *Handler {
	h := &Handler{
		recorder:    rec,
		connections: make(map[*websocket.Conn]struct{}),
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
		},
	}
	h.mux = buildRoutes(h)

	// There's no way to stop this go routine at the moment, but that's okay
	// since we only create one handler.
	go func() {
		for v := range requestInfoChan {
			message := WebsocketMessage{
				Type: "record",
				Data: v,
			}

			data, _ := json.Marshal(message)

			for conn := range h.connections {
				err := conn.WriteMessage(websocket.TextMessage, data)
				if err != nil {
					h.connMu.Lock()
					delete(h.connections, conn)
					h.connMu.Unlock()
				}
			}
		}
	}()

	return h
}

func buildRoutes(h *Handler) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("./static"))))

	mux.HandleFunc("/", serveIndex)
	mux.HandleFunc("/request", h.getRequestRecord)
	mux.HandleFunc("/ws", h.websocketHandler)

	return mux
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

var BadRequest = fmt.Errorf("bad request")

func (h *Handler) getRequestRecord(w http.ResponseWriter, r *http.Request) {
	record, err := h._getRequestRecord(r)

	switch {
	case errors.Is(err, BadRequest):
		w.WriteHeader(http.StatusBadRequest)
		return
	case err != nil:
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	data, _ := json.Marshal(record)

	w.Write(data)
}

func (h *Handler) _getRequestRecord(r *http.Request) (*Record, error) {
	requestIDString := r.URL.Query().Get("id")

	if requestIDString == "" {
		return nil, fmt.Errorf("%w, no id query param", BadRequest)
	}

	requestID, err := strconv.Atoi(requestIDString)

	if err != nil {
		return nil, fmt.Errorf("%w, invalid request id \"%s\"", BadRequest, requestIDString)
	}

	if requestID == 0 {
		return nil, fmt.Errorf("%w, invalid request id %d", BadRequest, requestID)
	}

	request, err := h.recorder.GetRequest(requestID)
	if err != nil {
		return nil, err
	}

	graphQLRequest, err := proxy.ParseRequest(request)
	if err != nil {
		return nil, err
	}

	response, err := h.recorder.GetResponse(requestID)
	if err != nil {
		return nil, err
	}

	snapshot, err := h.recorder.MaybeGetSnapshot(requestID)
	if err != nil {
		return nil, err
	}

	priorSnapshot, err := h.recorder.GetPriorSnapshot(requestID)
	if err != nil {
		return nil, err
	}

	// Requests that don't have their own snapshots use the snapshot from the
	// most recent request that has a snapshot as the "current snapshot".
	if snapshot == nil {
		snapshot = priorSnapshot
		priorSnapshot = nil
	}

	return &Record{
		RequestID:       requestID,
		OperationType:   graphQLRequest.OperationType,
		OperationName:   graphQLRequest.OperationName,
		Request:         string(request),
		Response:        string(response),
		CurrentSnapshot: string(snapshot),
		PriorSnapshot:   string(priorSnapshot),
	}, nil
}

func (h *Handler) websocketHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}
	if err != nil {
		log.Println(err)
		return
	}

	records, err := _getAllRequestInfo(h.recorder)
	if err != nil {
		log.Println(err)
		return
	}

	// When a web socket connection is first established we send info for all
	// existing requests to the client. This is the only time this go routine
	// writes to the connection. After sending this message, only the goroutine
	// that services the requestInfoChan writes to the connection.
	message := WebsocketMessage{
		Type: "init",
		Data: records,
	}
	data, _ := json.Marshal(message)

	err = conn.WriteMessage(websocket.TextMessage, data)
	if err != nil {
		return
	}

	h.connMu.Lock()
	h.connections[conn] = struct{}{}
	h.connMu.Unlock()

	fmt.Println("Open conn")
	for {
		// The only time read message should return is when the client closes
		// the connection. Currently there aren't any supported client to
		// server messages.
		_, _, err := conn.ReadMessage()
		if err != nil {
			fmt.Println("Close conn")
			h.connMu.Lock()
			delete(h.connections, conn)
			h.connMu.Unlock()
			return
		}
	}
}

func _getAllRequestInfo(rec *recorder.Recorder) ([]proxy.RequestInfo, error) {
	var records []proxy.RequestInfo

	requestIDs, err := rec.GetAllRequestIDs()
	if err != nil {
		return nil, err
	}

	for _, requestID := range requestIDs {
		request, err := rec.GetRequest(requestID)
		if err != nil {
			return nil, err
		}

		graphQLRequest, err := proxy.ParseRequest(request)
		if err != nil {
			return nil, err
		}

		snapshot, err := rec.MaybeGetSnapshot(requestID)
		if err != nil {
			return nil, err
		}

		records = append(records, proxy.RequestInfo{
			RequestID:        requestID,
			OperationType:    graphQLRequest.OperationType,
			OperationName:    graphQLRequest.OperationName,
			WillSnapshot:     snapshot != nil,
			ShapshotComplete: snapshot != nil,
		})
	}

	return records, nil
}

func serveIndex(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "./templates/index.html")
}
