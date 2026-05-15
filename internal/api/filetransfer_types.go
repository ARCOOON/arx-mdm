package api

// C2FileChunk is one chunk of a file streamed from an agent over the C2 WebSocket.
type C2FileChunk struct {
	Data []byte
	EOF  bool
	Err  string
}

// C2FileUploadResult is the agent acknowledgement for a streamed upload.
type C2FileUploadResult struct {
	OK    bool
	Error string
}
