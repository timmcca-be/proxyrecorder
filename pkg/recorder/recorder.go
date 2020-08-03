// Package recorder is responsible for managing recorded requests, responses
// and snapshots.
package recorder

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
)

type Recorder struct {
	RootPath string
}

type RecorderSaver interface {
	SaveRequest(requestID int, content []byte) error
	SaveResponse(requestID int, content []byte) error
	SaveSnapshot(requestID int, content []byte) error
	FormatRequestID(requestID int) string
	NextRequestID() (int, error)
}

func (r *Recorder) NextRequestID() (int, error) {
	requestIDs, err := r.GetAllRequestIDs()
	if err != nil {
		return 0, err
	}
	if len(requestIDs) == 0 {
		return 0, nil
	}
	return requestIDs[len(requestIDs)-1] + 1, nil
}

func (r *Recorder) GetAllRequestIDs() ([]int, error) {
	requestDirRegex := regexp.MustCompile(`request-(\d{6})`)
	requestDirs, err := ioutil.ReadDir(r.RootPath)
	if err != nil {
		return nil, err
	}
	if len(requestDirs) == 0 {
		return nil, nil
	}
	requestIDs := make([]int, 0, len(requestDirs))
	for _, requestDir := range requestDirs {
		matches := requestDirRegex.FindStringSubmatch(requestDir.Name())
		if len(matches) == 0 {
			return nil, fmt.Errorf("invalid request directory name \"%s\"", requestDir.Name())
		}
		id, err := strconv.Atoi(matches[1])
		if err != nil {
			return nil, err
		}
		if id != 0 {
			requestIDs = append(requestIDs, id)
		}
	}
	return requestIDs, nil
}

func (r *Recorder) SaveRequest(requestID int, content []byte) error {
	return r.saveFile(requestID, "request.txt", content)
}

func (r *Recorder) SaveResponse(requestID int, content []byte) error {
	return r.saveFile(requestID, "response.txt", content)
}

func (r *Recorder) SaveSnapshot(requestID int, content []byte) error {
	return r.saveFile(requestID, "snapshot.txt", content)
}

func (r *Recorder) GetRequest(requestID int) ([]byte, error) {
	return r.loadFile(requestID, "request.txt")
}

func (r *Recorder) GetResponse(requestID int) ([]byte, error) {
	return r.loadFile(requestID, "response.txt")
}

func (r *Recorder) MaybeGetSnapshot(requestID int) ([]byte, error) {
	snapshot, err := r.loadFile(requestID, "snapshot.txt")
	if os.IsNotExist(err) {
		return nil, nil
	}
	return snapshot, err
}

func (r *Recorder) GetPriorSnapshot(requestID int) ([]byte, error) {
	if requestID <= 0 {
		return nil, fmt.Errorf("invalid request ID, %d", requestID)
	}

	priorRequestID := requestID - 1

	for priorRequestID >= 0 {
		snapshot, err := r.MaybeGetSnapshot(priorRequestID)
		if err != nil {
			return nil, err
		}
		if snapshot != nil {
			return snapshot, nil
		}
		priorRequestID -= 1
	}

	return nil, fmt.Errorf("prior snapshot not found, requestID %d", requestID)
}

func (r *Recorder) saveFile(requestID int, filename string, content []byte) error {
	dir := r.requestPath(requestID)
	err := os.MkdirAll(dir, 0755)
	if err != nil {
		return err
	}
	path := filepath.Join(dir, filename)
	return ioutil.WriteFile(path, content, 0644)
}

func (r *Recorder) loadFile(requestID int, filename string) ([]byte, error) {
	dir := r.requestPath(requestID)
	path := filepath.Join(dir, filename)
	return ioutil.ReadFile(path)
}

func (r *Recorder) requestPath(requestID int) string {
	return filepath.Join(r.RootPath, "request-"+r.FormatRequestID(requestID))
}

func (r *Recorder) FormatRequestID(requestID int) string {
	return fmt.Sprintf("%06d", requestID)
}
