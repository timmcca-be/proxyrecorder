// Package server creates a proxy recorder server that listens on two ports:
// one port for the proxy and another port for the web tool.
package server

import (
	"context"
	"fmt"
	"net/http"

	"github.com/dnerdy/proxyrecorder/pkg/proxy"
	"github.com/dnerdy/proxyrecorder/pkg/recorder"
	"github.com/dnerdy/proxyrecorder/pkg/tool"
	"golang.org/x/sync/errgroup"
)

type Server struct {
	snapshotter proxy.Snapshotter
	selector    proxy.RequestSelector
	recorder    *recorder.Recorder
	reporter    proxy.Reporter
	mux         *http.ServeMux
}

type Reporter struct{}

func (r *Reporter) Report(label string, message string) {
	if label != "" {
		label = "[" + label + "]"
	}
	fmt.Printf("%-10s %s\n", label, message)
}

func NewServer(
	snapshotter proxy.Snapshotter,
	selector proxy.RequestSelector,
	rec *recorder.Recorder,
) *Server {
	return &Server{
		snapshotter: snapshotter,
		selector:    selector,
		recorder:    rec,
		reporter:    &Reporter{},
	}
}

func (s *Server) ListenAndServe(ctx context.Context) error {
	proxyPort := 7081
	toolPort := 1234

	requestInfoChan := make(chan proxy.RequestInfo)

	proxyHandler, err := proxy.NewHandler(
		fmt.Sprintf("127.0.0.1:%d", proxyPort),
		"http://127.0.0.1:8081",
		s.snapshotter,
		s.recorder,
		s.selector,
		s.reporter,
		requestInfoChan,
	)
	if err != nil {
		return err
	}
	err = s.takeInitialSnapshotIfNeeded()
	if err != nil {
		return err
	}
	toolHandler := tool.NewHandlerAndStartWebsocketWorker(s.recorder, requestInfoChan)

	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		return http.ListenAndServe(
			fmt.Sprintf(":%d", proxyPort),
			proxyHandler,
		)
	})

	g.Go(func() error {
		return http.ListenAndServe(
			fmt.Sprintf(":%d", toolPort),
			toolHandler,
		)
	})

	fmt.Printf("tool:  listening on http://127.0.0.1:%d\n", toolPort)
	fmt.Printf("proxy: listening on http://127.0.0.1:%d\n", proxyPort)

	return g.Wait()
}

func (s *Server) takeInitialSnapshotIfNeeded() error {
	nextRequestID, err := s.recorder.NextRequestID()
	if err != nil {
		return err
	}
	if nextRequestID != 0 {
		return nil
	}
	return s.takeSnapshot(0, proxy.GraphQLRequest{})
}

func (s *Server) takeSnapshot(requestID int, graphQLRequest proxy.GraphQLRequest) error {
	s.reporter.Report("", fmt.Sprintf("taking a snapshot, %s...", s.snapshotter.SnapshotInfo()))
	snapshot, err := s.snapshotter.TakeSnapshot(graphQLRequest)
	if err != nil {
		return err
	} else {
		s.reporter.Report("", "...done")
	}
	s.recorder.SaveSnapshot(requestID, snapshot)
	return nil
}
