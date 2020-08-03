package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/dnerdy/proxyrecorder/pkg/proxy"
	"github.com/dnerdy/proxyrecorder/pkg/recorder"
	"github.com/dnerdy/proxyrecorder/pkg/server"
)

func printUsageAndExit() {
	fmt.Println(`usage: proxyrecorder <record-dir> <webapp> <kaid> <exam-group-id>
 `)
	os.Exit(1)
}

// TODO: record the kaid in the record directory?
// TODO: record the examGroupID in the record directory?

func main() {
	ctx := context.Background()

	if len(os.Args) != 5 {
		printUsageAndExit()
	}

	cwd, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	recordPath := os.Args[1]
	webappPath := os.Args[2]
	kaid := os.Args[3]
	examGroupID := os.Args[4]

	recordPath = filepath.Join(cwd, recordPath)

	err = os.MkdirAll(recordPath, 0755)
	if err != nil {
		panic(err)
	}

	snapshotter := &Snapshotter{
		webappPath,
		kaid,
		examGroupID,
	}
	requestRecorder := &recorder.Recorder{
		RootPath: recordPath,
	}
	selector := &RequestSelector{}

	s := server.NewServer(
		snapshotter,
		selector,
		requestRecorder,
	)
	log.Fatal(s.ListenAndServe(ctx))
}

type RequestSelector struct{}

func (s *RequestSelector) ShouldRecordRequest(r proxy.GraphQLRequest) bool {
	return r.OperationName == "createGtpUserDataIfMissing" ||
		strings.HasPrefix(r.OperationName, "gtp_")
}

func (s *RequestSelector) ShouldSnapshotRequest(r proxy.GraphQLRequest) bool {
	return r.OperationType == proxy.OperationTypeMutation ||
		r.OperationName == "gtp_getTaskByDescriptor"
}

type Snapshotter struct {
	webappPath  string
	kaid        string
	examGroupID string
}

func (s *Snapshotter) TakeSnapshot(_ proxy.GraphQLRequest) ([]byte, error) {
	tmpfile, err := ioutil.TempFile("", "snapshot.pickle")
	if err != nil {
		return nil, err
	}

	defer os.Remove(tmpfile.Name()) // clean up

	cmd := exec.Command(
		"tools/devshell.py",
		"--script",
		"test_prep/tools/dump_user_data.py",
		s.examGroupID,
		s.kaid,
		tmpfile.Name(),
	)
	cmd.Dir = s.webappPath
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, out)
	}
	cmd = exec.Command(
		"test_prep/tools/pickle_print.py",
		tmpfile.Name(),
	)
	cmd.Dir = s.webappPath
	out, err = cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, out)
	}

	return out, nil
}

func (s *Snapshotter) SnapshotInfo() string {
	return fmt.Sprintf("kaid: %s, examGroupID: %s", s.kaid, s.examGroupID)
}
