package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"io"
	"net/http"
	"strings"
	"time"
)

type RelayRequest struct {
	Uuid    uuid.UUID
	Method  string
	Url     string
	Headers map[string][]string
	Body    []byte
	Retries uint8
}

func RunWorker(queue *Queue, baseUrl string, victoriaLogsUrl string, debug bool) {
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	var batcher *vlBatcher
	if victoriaLogsUrl != "" {
		batcher = newVlBatcher(client, victoriaLogsUrl, debug)
	}

	for {
		for {
			relayRequest := queue.Get()
			if relayRequest == nil {
				break
			}

			relayRequest.Retries = relayRequest.Retries + 1

			// prepare request
			err := sendRequest(client, baseUrl, relayRequest)
			if err != nil {
				// fatal error
				fmt.Println("[WORKER] ERROR send request", relayRequest.Uuid, ": ", relayRequest.Url, relayRequest.Retries, err)

				if relayRequest.Retries > 5 {
					// max 5 retries
					fmt.Println("[WORKER] Removed from queue: ", relayRequest.Url)
					break
				}

				// put it back to queue
				queue.RePush(relayRequest)

				// wait 10 sec -> server is down?
				time.Sleep(time.Second * 10)
				break
			} else {
				if debug {
					fmt.Println("[WORKER] SUCCESS send request", relayRequest.Uuid.String(), ": ", relayRequest.Url)
				}

				if batcher != nil {
					if kind := victoriaLogsKind(relayRequest); kind != vlKindNone {
						batcher.Add(kind, relayRequest)
					}
					batcher.FlushIfDue()
				}
			}
		}

		if batcher != nil {
			batcher.FlushIfDue()
		}

		client.CloseIdleConnections()
		// fmt.Println("🧲 loop")

		// sleep 1 sec
		time.Sleep(time.Second)
	}
}

const (
	vlKindNone = ""
	vlKindBulk = "bulk"
	vlKindDoc  = "doc"
)

func victoriaLogsKind(relayRequest *RelayRequest) string {
	if relayRequest.Method != "POST" {
		return vlKindNone
	}
	parts := splitRequestPath(relayRequest.Url)
	// /_bulk or /<index>/_bulk
	if len(parts) <= 2 && parts[len(parts)-1] == "_bulk" {
		return vlKindBulk
	}
	// /<index>/_doc/<id>
	if len(parts) == 3 && parts[1] == "_doc" {
		return vlKindDoc
	}
	return vlKindNone
}

// RequestURI might have query params.
func splitRequestPath(url string) []string {
	if idx := strings.IndexAny(url, "?#"); idx != -1 {
		url = url[:idx]
	}
	return strings.Split(strings.Trim(url, "/"), "/")
}

func buildVictoriaLogsBody(kind string, relayRequest *RelayRequest) ([]byte, error) {
	// the batch buffer holds plain NDJSON, so compressed bodies must be decoded first
	rawBody, err := decodeRequestBody(relayRequest)
	if err != nil {
		return nil, err
	}

	parts := splitRequestPath(relayRequest.Url)

	if kind == vlKindBulk {
		// bulk body is already in the NDJSON format VictoriaLogs accepts
		defaultIndex := ""
		if len(parts) == 2 {
			// /<index>/_bulk
			defaultIndex = parts[0]
		}
		return transformBulkBody(rawBody, defaultIndex), nil
	}

	// single _doc insert -> wrap into a one-item bulk payload
	index := parts[0]

	var doc json.RawMessage
	if err := json.Unmarshal(rawBody, &doc); err != nil {
		return nil, fmt.Errorf("invalid document body: %w", err)
	}
	docJson, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	docJson = transformDocLine(docJson, index)

	action, err := json.Marshal(map[string]map[string]string{
		"create": {"_index": index},
	})
	if err != nil {
		return nil, err
	}

	var body bytes.Buffer
	body.Write(action)
	body.WriteByte('\n')
	body.Write(docJson)
	body.WriteByte('\n')
	return body.Bytes(), nil
}

// VictoriaLogs requires the _msg field in every document
const vlDefaultMsg = "missing _msg"

// Adds the _msg field (fallback: "message" field, then vlDefaultMsg) and the
// "index" stream field with the ES index name.
func transformDocLine(doc []byte, index string) []byte {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(doc, &fields); err != nil {
		// not an object -> leave it to VictoriaLogs
		return doc
	}

	changed := false

	if _, ok := fields["_msg"]; !ok {
		// fallback to the "message" field (only when it is a string)
		if message, ok := fields["message"]; ok && len(message) > 0 && message[0] == '"' {
			fields["_msg"] = message
		} else {
			fields["_msg"] = json.RawMessage(`"` + vlDefaultMsg + `"`)
		}
		changed = true
	}

	if index != "" {
		if indexJson, err := json.Marshal(index); err == nil {
			fields["index"] = indexJson
			changed = true
		}
	}

	if !changed {
		return doc
	}
	out, err := json.Marshal(fields)
	if err != nil {
		return doc
	}
	return out
}

// Transforms document lines of a bulk body via transformDocLine. Action lines
// (create/index/delete/...) are kept as they are; the ES index is taken from
// the action line's _index, or from defaultIndex (/<index>/_bulk requests).
func transformBulkBody(rawBody []byte, defaultIndex string) []byte {
	var out bytes.Buffer
	isDocLine := false
	docIndex := defaultIndex
	for _, line := range bytes.Split(rawBody, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}

		if isDocLine {
			line = transformDocLine(line, docIndex)
			isDocLine = false
		} else {
			// action line; delete is not followed by a document line
			var action map[string]struct {
				Index string `json:"_index"`
			}
			isDelete := false
			docIndex = defaultIndex
			if err := json.Unmarshal(line, &action); err == nil {
				_, isDelete = action["delete"]
				for _, meta := range action {
					if meta.Index != "" {
						docIndex = meta.Index
					}
				}
			}
			isDocLine = !isDelete
		}

		out.Write(line)
		out.WriteByte('\n')
	}
	return out.Bytes()
}

func decodeRequestBody(relayRequest *RelayRequest) ([]byte, error) {
	encodings := relayRequest.Headers["Content-Encoding"]
	if len(encodings) == 0 {
		return relayRequest.Body, nil
	}

	switch encoding := encodings[0]; encoding {
	case "", "identity":
		return relayRequest.Body, nil
	case "gzip":
		reader, err := gzip.NewReader(bytes.NewReader(relayRequest.Body))
		if err != nil {
			return nil, err
		}
		defer reader.Close()
		return io.ReadAll(reader)
	default:
		return nil, fmt.Errorf("unsupported Content-Encoding: %s", encoding)
	}
}

const (
	vlFlushInterval  = 30 * time.Second
	vlFlushSizeLimit = 4 * 1024 * 1024
	// when VictoriaLogs is down, the unsent buffer is dropped above this size
	vlBufferMaxSize = 32 * 1024 * 1024
)

// Batches NDJSON lines of multiple requests into a single bulk request to
// VictoriaLogs. Used only from the worker goroutine -> no locking.
type vlBatcher struct {
	client    *http.Client
	url       string
	buf       bytes.Buffer
	lastFlush time.Time
	debug     bool
}

func newVlBatcher(client *http.Client, victoriaLogsUrl string, debug bool) *vlBatcher {
	return &vlBatcher{
		client:    client,
		url:       strings.TrimRight(victoriaLogsUrl, "/") + "/insert/elasticsearch/_bulk?_stream_fields=index",
		lastFlush: time.Now(),
		debug:     debug,
	}
}

func (b *vlBatcher) Add(kind string, relayRequest *RelayRequest) {
	body, err := buildVictoriaLogsBody(kind, relayRequest)
	if err != nil {
		fmt.Println("[WORKER] ERROR build VictoriaLogs body", relayRequest.Uuid, ": ", relayRequest.Url, err)
		return
	}
	if len(body) == 0 {
		return
	}

	b.buf.Write(body)
	if body[len(body)-1] != '\n' {
		b.buf.WriteByte('\n')
	}

	if b.buf.Len() >= vlFlushSizeLimit {
		b.Flush()
	}
}

func (b *vlBatcher) FlushIfDue() {
	if b.buf.Len() > 0 && time.Since(b.lastFlush) >= vlFlushInterval {
		b.Flush()
	}
}

func (b *vlBatcher) Flush() {
	if b.buf.Len() == 0 {
		return
	}

	// both on success and on error: next flush attempt one interval from now
	b.lastFlush = time.Now()

	if err := b.send(); err != nil {
		fmt.Println("[WORKER] ERROR send batch to VictoriaLogs: ", err)

		// keep the buffer for the next flush attempt, unless it is too big
		if b.buf.Len() > vlBufferMaxSize {
			fmt.Println("[WORKER] VictoriaLogs buffer overflow, dropping", b.buf.Len(), "bytes")
			b.buf.Reset()
		}
		return
	}

	if b.debug {
		fmt.Println("[WORKER] SUCCESS send batch to VictoriaLogs: ", b.buf.Len(), "bytes")
	}
	b.buf.Reset()
}

func (b *vlBatcher) send() error {
	req, err := http.NewRequest("POST", b.url, bytes.NewReader(b.buf.Bytes()))
	if err != nil {
		return err
	}
	// Authorization is intentionally not copied from the original requests:
	// the http client fills in basic auth from the userinfo in the url.
	req.Header.Set("Content-Type", "application/json")

	res, err := b.client.Do(req)
	if res != nil {
		defer res.Body.Close()
	}
	if err != nil {
		return err
	}

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		resBody, _ := io.ReadAll(res.Body)
		return fmt.Errorf("invalid response: %d %s", res.StatusCode, resBody)
	}

	_, _ = io.Copy(io.Discard, res.Body)
	return nil
}
