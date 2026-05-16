package ws

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/ARCOOON/arx-mdm/pkg/system"
)

const (
	agentMsgFsListDirResult    = "fs_listdir_result"
	agentMsgNetListResult      = "net_list_result"
	agentMsgHostnameSetResult  = "hostname_set_result"
	maxAgentUploadBytes        = 512 << 20
	defaultFSDownloadChunkSize = 256 * 1024
)

func (rt *agentRuntime) handleFsListDir(data []byte) error {
	var cmd struct {
		RequestID string `json:"request_id"`
		Path      string `json:"path"`
	}
	if err := json.Unmarshal(data, &cmd); err != nil {
		return err
	}
	ents, err := system.ListDir(cmd.Path)
	if err != nil {
		return rt.writeJSON(map[string]any{
			"type":       agentMsgFsListDirResult,
			"request_id": strings.TrimSpace(cmd.RequestID),
			"ok":         false,
			"error":      err.Error(),
		})
	}
	return rt.writeJSON(map[string]any{
		"type":       agentMsgFsListDirResult,
		"request_id": strings.TrimSpace(cmd.RequestID),
		"ok":         true,
		"path":       strings.TrimSpace(cmd.Path),
		"entries":    ents,
	})
}

func (rt *agentRuntime) handleNetList(data []byte) error {
	var cmd struct {
		RequestID string `json:"request_id"`
	}
	_ = json.Unmarshal(data, &cmd)
	ifs, err := system.ListNetworkInterfaces()
	if err != nil {
		return rt.writeJSON(map[string]any{
			"type":       agentMsgNetListResult,
			"request_id": strings.TrimSpace(cmd.RequestID),
			"ok":         false,
			"error":      err.Error(),
		})
	}
	return rt.writeJSON(map[string]any{
		"type":       agentMsgNetListResult,
		"request_id": strings.TrimSpace(cmd.RequestID),
		"ok":         true,
		"interfaces": ifs,
	})
}

func (rt *agentRuntime) handleHostnameSet(data []byte) error {
	var cmd struct {
		RequestID string `json:"request_id"`
		Hostname  string `json:"hostname"`
	}
	if err := json.Unmarshal(data, &cmd); err != nil {
		return err
	}
	host := strings.TrimSpace(cmd.Hostname)
	if err := system.SetHostname(host); err != nil {
		return rt.writeJSON(map[string]any{
			"type":       agentMsgHostnameSetResult,
			"request_id": strings.TrimSpace(cmd.RequestID),
			"ok":         false,
			"error":      err.Error(),
		})
	}
	return rt.writeJSON(map[string]any{
		"type":       agentMsgHostnameSetResult,
		"request_id": strings.TrimSpace(cmd.RequestID),
		"ok":         true,
		"hostname":   host,
	})
}

type fsDownloadCmd struct {
	Action    string `json:"action"`
	RequestID string `json:"request_id"`
	Path      string `json:"path"`
	ChunkSize int    `json:"chunk_size"`
}

func (rt *agentRuntime) handleFsDownload(data []byte) error {
	var cmd fsDownloadCmd
	if err := json.Unmarshal(data, &cmd); err != nil {
		return err
	}
	go rt.runFsDownload(cmd)
	return nil
}

func (rt *agentRuntime) runFsDownload(cmd fsDownloadCmd) {
	rid := strings.TrimSpace(cmd.RequestID)
	chunkSize := cmd.ChunkSize
	if chunkSize <= 0 || chunkSize > 1024*1024 {
		chunkSize = defaultFSDownloadChunkSize
	}
	path := strings.TrimSpace(cmd.Path)
	f, err := os.Open(path)
	if err != nil {
		_ = rt.writeJSON(map[string]any{
			"type":       agentMsgFSDownloadErr,
			"request_id": rid,
			"error":      err.Error(),
		})
		return
	}
	defer f.Close()

	buf := make([]byte, chunkSize)
	for {
		n, readErr := f.Read(buf)
		if n > 0 {
			eof := errors.Is(readErr, io.EOF)
			if err := rt.writeJSON(map[string]any{
				"type":       agentMsgFSDownloadChunk,
				"request_id": rid,
				"data_b64":   base64.StdEncoding.EncodeToString(buf[:n]),
				"eof":        eof,
			}); err != nil {
				return
			}
			if eof {
				return
			}
			continue
		}
		if errors.Is(readErr, io.EOF) {
			_ = rt.writeJSON(map[string]any{
				"type":       agentMsgFSDownloadChunk,
				"request_id": rid,
				"data_b64":   "",
				"eof":        true,
			})
			return
		}
		if readErr != nil {
			_ = rt.writeJSON(map[string]any{
				"type":       agentMsgFSDownloadErr,
				"request_id": rid,
				"error":      readErr.Error(),
			})
			return
		}
	}
}

// --- upload session (per request_id) ---

type fsUploadState struct {
	mu      sync.Mutex
	files   map[string]*os.File
	written map[string]int64
}

func (s *fsUploadState) begin(rid, path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.files == nil {
		s.files = make(map[string]*os.File)
		s.written = make(map[string]int64)
	}
	if _, ok := s.files[rid]; ok {
		return errors.New("upload session already active for request_id")
	}
	f, err := system.OpenFileWriteTrunc(path, 0o644)
	if err != nil {
		return err
	}
	s.files[rid] = f
	s.written[rid] = 0
	return nil
}

func (s *fsUploadState) chunk(rid string, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.files[rid]
	if f == nil {
		return os.ErrInvalid
	}
	n, err := f.Write(data)
	if err != nil {
		return err
	}
	s.written[rid] += int64(n)
	if s.written[rid] > maxAgentUploadBytes {
		return os.ErrInvalid
	}
	return nil
}

func (s *fsUploadState) finish(rid string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.files[rid]
	if f == nil {
		return os.ErrInvalid
	}
	err := f.Sync()
	cerr := f.Close()
	delete(s.files, rid)
	delete(s.written, rid)
	if err != nil {
		return err
	}
	return cerr
}

func (s *fsUploadState) abort(rid string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if f := s.files[rid]; f != nil {
		_ = f.Close()
		delete(s.files, rid)
		delete(s.written, rid)
	}
}

var globalFsUpload fsUploadState

func (rt *agentRuntime) handleFsUploadBegin(data []byte) error {
	var cmd struct {
		RequestID string `json:"request_id"`
		Path      string `json:"path"`
	}
	if err := json.Unmarshal(data, &cmd); err != nil {
		return err
	}
	rid := strings.TrimSpace(cmd.RequestID)
	path := strings.TrimSpace(cmd.Path)
	if rid == "" || path == "" {
		return rt.fsUploadReply(rid, false, "request_id and path are required")
	}
	if err := globalFsUpload.begin(rid, path); err != nil {
		return rt.fsUploadReply(rid, false, err.Error())
	}
	return nil
}

func (rt *agentRuntime) handleFsUploadChunk(data []byte) error {
	var cmd struct {
		RequestID string `json:"request_id"`
		DataB64   string `json:"data_b64"`
	}
	if err := json.Unmarshal(data, &cmd); err != nil {
		return err
	}
	rid := strings.TrimSpace(cmd.RequestID)
	raw, err := base64.StdEncoding.DecodeString(cmd.DataB64)
	if err != nil {
		globalFsUpload.abort(rid)
		return rt.fsUploadReply(rid, false, "invalid base64: "+err.Error())
	}
	if err := globalFsUpload.chunk(rid, raw); err != nil {
		globalFsUpload.abort(rid)
		return rt.fsUploadReply(rid, false, err.Error())
	}
	return nil
}

func (rt *agentRuntime) handleFsUploadFinish(data []byte) error {
	var cmd struct {
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(data, &cmd); err != nil {
		return err
	}
	rid := strings.TrimSpace(cmd.RequestID)
	if err := globalFsUpload.finish(rid); err != nil {
		return rt.fsUploadReply(rid, false, err.Error())
	}
	return rt.fsUploadReply(rid, true, "")
}

func (rt *agentRuntime) handleFsUploadAbort(data []byte) error {
	var cmd struct {
		RequestID string `json:"request_id"`
	}
	_ = json.Unmarshal(data, &cmd)
	rid := strings.TrimSpace(cmd.RequestID)
	globalFsUpload.abort(rid)
	return nil
}

func (rt *agentRuntime) fsUploadReply(requestID string, ok bool, errMsg string) error {
	return rt.writeJSON(map[string]any{
		"type":       agentMsgFSUploadResult,
		"request_id": requestID,
		"ok":         ok,
		"error":      errMsg,
	})
}
