package ws

import (
	"encoding/base64"
	"encoding/json"
	"strings"

	"github.com/ARCOOON/arx-mdm/internal/api"
)

// Agent → server file transfer wire types (not broadcast to dashboards).
const (
	agentMsgFSDownloadChunk = "fs_download_chunk"
	agentMsgFSDownloadDone  = "fs_download_done"
	agentMsgFSDownloadErr   = "fs_download_error"
	agentMsgFSUploadResult  = "fs_upload_result"
)

type fsDownloadWaiter struct {
	ch chan api.C2FileChunk
}

func fsWaitKey(certSerial, requestID string) string {
	return strings.TrimSpace(certSerial) + "\x1e" + strings.TrimSpace(requestID)
}

// RegisterFSDownloadWaiter subscribes to chunked download messages for a cert serial + request id pair.
// The caller must invoke cancel when finished to unregister (typically after EOF or error).
func (h *Hub) RegisterFSDownloadWaiter(certSerial, requestID string) (ch <-chan api.C2FileChunk, cancel func()) {
	if h == nil {
		return nil, func() {}
	}
	c := make(chan api.C2FileChunk, 8)
	key := fsWaitKey(certSerial, requestID)
	h.fsMu.Lock()
	if h.fsDown == nil {
		h.fsDown = make(map[string]*fsDownloadWaiter)
	}
	h.fsDown[key] = &fsDownloadWaiter{ch: c}
	h.fsMu.Unlock()
	cancel = func() {
		h.fsMu.Lock()
		delete(h.fsDown, key)
		h.fsMu.Unlock()
	}
	return c, cancel
}

// RegisterFSUploadResultWaiter waits for a single fs_upload_result from the agent.
func (h *Hub) RegisterFSUploadResultWaiter(certSerial, requestID string) (ch <-chan api.C2FileUploadResult, cancel func()) {
	if h == nil {
		return nil, func() {}
	}
	c := make(chan api.C2FileUploadResult, 1)
	key := fsWaitKey(certSerial, requestID)
	h.fsMu.Lock()
	if h.fsUp == nil {
		h.fsUp = make(map[string]chan api.C2FileUploadResult)
	}
	h.fsUp[key] = c
	h.fsMu.Unlock()
	cancel = func() {
		h.fsMu.Lock()
		delete(h.fsUp, key)
		h.fsMu.Unlock()
	}
	return c, cancel
}

// TryDeliverFileTransfer routes agent-originated file proxy messages to REST waiters.
func (h *Hub) TryDeliverFileTransfer(certSerial string, data []byte) bool {
	if h == nil {
		return false
	}
	var probe struct {
		Type      string `json:"type"`
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return false
	}
	t := strings.TrimSpace(probe.Type)
	rid := strings.TrimSpace(probe.RequestID)
	switch t {
	case agentMsgFSDownloadChunk, agentMsgFSDownloadDone, agentMsgFSDownloadErr:
		if rid == "" {
			return false
		}
		return h.deliverFSDownload(certSerial, rid, t, data)
	case agentMsgFSUploadResult:
		if rid == "" {
			return false
		}
		return h.deliverFSUploadResult(certSerial, rid, data)
	default:
		return false
	}
}

func (h *Hub) deliverFSDownload(certSerial, requestID, msgType string, data []byte) bool {
	key := fsWaitKey(certSerial, requestID)
	h.fsMu.Lock()
	w := h.fsDown[key]
	h.fsMu.Unlock()
	if w == nil {
		return true
	}
	switch msgType {
	case agentMsgFSDownloadChunk:
		var m struct {
			DataB64 string `json:"data_b64"`
			EOF     bool   `json:"eof"`
		}
		if err := json.Unmarshal(data, &m); err != nil {
			trySendFSDownload(w.ch, api.C2FileChunk{Err: err.Error()})
			return true
		}
		var raw []byte
		if strings.TrimSpace(m.DataB64) != "" {
			var err error
			raw, err = base64.StdEncoding.DecodeString(m.DataB64)
			if err != nil {
				trySendFSDownload(w.ch, api.C2FileChunk{Err: err.Error()})
				return true
			}
		}
		trySendFSDownload(w.ch, api.C2FileChunk{Data: raw, EOF: m.EOF})
		return true
	case agentMsgFSDownloadDone:
		trySendFSDownload(w.ch, api.C2FileChunk{EOF: true})
		return true
	case agentMsgFSDownloadErr:
		var m struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(data, &m)
		trySendFSDownload(w.ch, api.C2FileChunk{Err: strings.TrimSpace(m.Error)})
		return true
	default:
		return false
	}
}

func trySendFSDownload(ch chan api.C2FileChunk, v api.C2FileChunk) {
	select {
	case ch <- v:
	default:
	}
}

func (h *Hub) deliverFSUploadResult(certSerial, requestID string, data []byte) bool {
	var m struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	_ = json.Unmarshal(data, &m)
	key := fsWaitKey(certSerial, requestID)
	h.fsMu.Lock()
	ch := h.fsUp[key]
	h.fsMu.Unlock()
	if ch == nil {
		return true
	}
	select {
	case ch <- api.C2FileUploadResult{OK: m.OK, Error: strings.TrimSpace(m.Error)}:
	default:
	}
	return true
}
