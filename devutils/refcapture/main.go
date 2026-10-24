// Command refcapture is the capture harness for the reference's tool result
// texts.
//
// It is a mock of the CloudCode endpoint the reference CLI talks to
// (POST <CLOUD_CODE_URL>/v1internal:streamGenerateContent?alt=sse). Point the
// real `agy` at it and the reference executes its own tools: the harness
// answers the first request with one functionCall, then reads the
// functionResponse the reference sends back in the follow-up request. That
// functionResponse.response.output is the text the reference hands to the
// model — the thing this repository records as a 【一手】 fixture.
//
// It is a research tool, not part of the clone: it never runs inside
// pigasus.
//
// Usage:
//
//	go run ./devutils/refcapture -script script.json -out capture.json
//	# prints "listening http://127.0.0.1:PORT" on stdout
//	CLOUD_CODE_URL=http://127.0.0.1:PORT agy -p "go" --dangerously-skip-permissions
//
// script.json is a JSON array of call steps:
//
//	[{"name": "read_file", "args": {"Path": "/tmp/x.txt"}, "note": "free text"}]
//
// out.json receives one record per captured functionResponse.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// step is one scripted functionCall.
type step struct {
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
	Note string          `json:"note"`
}

// record is one captured functionResponse.
type record struct {
	Step    int    `json:"step"`
	Name    string `json:"name"`
	Note    string `json:"note"`
	Args    string `json:"args"`
	Output  string `json:"output"`
	HasCall bool   `json:"has_function_call"`
}

// ---------------------------------------------------------------------------
// wire shapes (protojson names of google.gca.aicode.v1main.GenerateContentResponse)
// ---------------------------------------------------------------------------

type frame struct {
	Response ccResponse `json:"response"`
	TraceID  string     `json:"traceId"`
}

type ccResponse struct {
	Candidates    []candidate `json:"candidates"`
	UsageMetadata usage       `json:"usageMetadata"`
	ModelVersion  string      `json:"modelVersion"`
}

type usage struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
	ThoughtsTokenCount   int `json:"thoughtsTokenCount"`
}

type candidate struct {
	Content struct {
		Role  string `json:"role"`
		Parts []part `json:"parts"`
	} `json:"content"`
	FinishReason string `json:"finishReason"`
}

type part struct {
	Text             string         `json:"text,omitempty"`
	Thought          bool           `json:"thought,omitempty"`
	ThoughtSignature string         `json:"thoughtSignature,omitempty"`
	FunctionCall     *functionCall  `json:"functionCall,omitempty"`
	FunctionResponse *functionReply `json:"functionResponse,omitempty"`
}

type functionCall struct {
	ID   string          `json:"id,omitempty"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args,omitempty"`
}

type functionReply struct {
	ID       string         `json:"id,omitempty"`
	Name     string         `json:"name"`
	Response map[string]any `json:"response"`
}

// request is the envelope the reference sends: {project, requestId, request, …}.
type request struct {
	Project     string `json:"project"`
	RequestType string `json:"requestType"`
	Request     struct {
		Contents []struct {
			Role  string `json:"role"`
			Parts []part `json:"parts"`
		} `json:"contents"`
	} `json:"request"`
	Model string `json:"model"`
}

func main() {
	var (
		scriptPath = flag.String("script", "", "JSON file with the scripted functionCalls")
		outPath    = flag.String("out", "", "JSON file that receives the captured results")
		addr       = flag.String("addr", "127.0.0.1:0", "listen address")
		rawDir     = flag.String("raw", "", "optional directory that receives the raw request bodies")
		upstream   = flag.String("upstream", "", "real CloudCode base URL; every non-intercepted RPC is forwarded to it")
	)
	flag.Parse()

	if *scriptPath == "" {
		fmt.Fprintln(os.Stderr, "refcapture: -script is required")
		os.Exit(2)
	}
	raw, err := os.ReadFile(*scriptPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "refcapture:", err)
		os.Exit(1)
	}
	var steps []step
	if err := json.Unmarshal(raw, &steps); err != nil {
		fmt.Fprintln(os.Stderr, "refcapture: parse script:", err)
		os.Exit(1)
	}

	h := &handler{steps: steps, outPath: *outPath, rawDir: *rawDir, upstream: *upstream}

	mux := http.NewServeMux()
	mux.HandleFunc("/", h.serve)

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "refcapture:", err)
		os.Exit(1)
	}
	fmt.Printf("listening http://%s\n", ln.Addr())
	os.Stdout.Sync()
	if err := http.Serve(ln, mux); err != nil {
		fmt.Fprintln(os.Stderr, "refcapture:", err)
		os.Exit(1)
	}
}

type handler struct {
	mu       sync.Mutex
	steps    []step
	next     int // index of the next step to issue
	records  []record
	outPath  string
	rawDir   string
	upstream string
	nReq     int
	client   *http.Client
}

func (h *handler) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)

	h.mu.Lock()
	h.nReq++
	n := h.nReq
	h.mu.Unlock()

	if h.rawDir != "" {
		_ = os.MkdirAll(h.rawDir, 0o755)
		_ = os.WriteFile(filepath.Join(h.rawDir, fmt.Sprintf("req-%02d.json", n)), body, 0o644)
	}
	fmt.Fprintf(os.Stderr, "[refcapture] %s %s (%d bytes)\n", r.Method, r.URL.Path, len(body))

	if !strings.Contains(strings.ToLower(r.URL.Path), "streamgeneratecontent") {
		if h.upstream != "" {
			h.forward(w, r, body)
			return
		}
		// Any other endpoint the reference reaches: answer 200 with an empty
		// object so the client does not treat it as a hard failure.
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
		return
	}

	var req request
	_ = json.Unmarshal(body, &req)

	// The reference also uses streamGenerateContent for side calls (the
	// conversation-title generator runs with requestType "checkpoint"). Only
	// the "agent" request type is the real turn; the side calls get a plain
	// text answer so they do not eat a scripted step.
	if req.RequestType != "agent" {
		fmt.Fprintf(os.Stderr, "[refcapture] side call %s (model %s) -> text\n", req.RequestType, req.Model)
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		flusher, _ := w.(http.Flusher)
		writeFrame(w, flusher, textFrame("Captured"))
		return
	}

	// Collect the functionResponses the reference sent back.
	var got []record
	for _, c := range req.Request.Contents {
		for _, p := range c.Parts {
			if p.FunctionResponse == nil {
				continue
			}
			out, _ := p.FunctionResponse.Response["output"].(string)
			got = append(got, record{
				Name:    p.FunctionResponse.Name,
				Output:  out,
				HasCall: p.FunctionResponse.ID != "",
			})
		}
	}

	h.mu.Lock()
	for _, g := range got {
		stepIdx := h.next - 1
		if stepIdx >= 0 && stepIdx < len(h.steps) {
			g.Step = stepIdx
			g.Args = string(h.steps[stepIdx].Args)
			g.Note = h.steps[stepIdx].Note
		}
		h.records = append(h.records, g)
	}
	h.writeOutLocked()
	h.mu.Unlock()

	// Answer with the next scripted call, or with a closing text once the
	// script is exhausted.
	h.mu.Lock()
	var next *step
	if h.next < len(h.steps) {
		next = &h.steps[h.next]
		h.next++
	}
	h.mu.Unlock()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	flusher, _ := w.(http.Flusher)

	if next == nil {
		writeFrame(w, flusher, textFrame("done"))
		return
	}
	writeFrame(w, flusher, callFrame(next, h.next))
}

// forward relays one RPC to the real CloudCode endpoint, verbatim.
func (h *handler) forward(w http.ResponseWriter, r *http.Request, body []byte) {
	if h.client == nil {
		h.client = &http.Client{}
	}
	u := strings.TrimRight(h.upstream, "/") + r.URL.Path
	if r.URL.RawQuery != "" {
		u += "?" + r.URL.RawQuery
	}
	req, err := http.NewRequestWithContext(r.Context(), r.Method, u, bytes.NewReader(body))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	for k, vs := range r.Header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	resp, err := h.client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[refcapture] upstream %s: %v\n", r.URL.Path, err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// writeOutLocked persists the records collected so far.
func (h *handler) writeOutLocked() {
	if h.outPath == "" {
		return
	}
	b, err := json.MarshalIndent(h.records, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(h.outPath, append(b, '\n'), 0o644)
}

// textFrame builds a closing assistant turn.
func textFrame(text string) frame {
	var f frame
	var c candidate
	c.Content.Role = "model"
	c.Content.Parts = []part{{Text: text}}
	c.FinishReason = "STOP"
	f.Response.Candidates = []candidate{c}
	f.Response.ModelVersion = "refcapture"
	f.Response.UsageMetadata = usage{PromptTokenCount: 1, CandidatesTokenCount: 1, TotalTokenCount: 2}
	f.TraceID = "refcapture"
	return f
}

// callFrame builds a turn that asks for one tool call.
func callFrame(s *step, idx int) frame {
	var f frame
	var c candidate
	c.Content.Role = "model"
	c.Content.Parts = []part{{
		ThoughtSignature: "c2lnay1fZw==",
		FunctionCall: &functionCall{
			ID:   fmt.Sprintf("call_%d", idx),
			Name: s.Name,
			Args: s.Args,
		},
	}}
	c.FinishReason = "STOP"
	f.Response.Candidates = []candidate{c}
	f.Response.ModelVersion = "refcapture"
	f.Response.UsageMetadata = usage{PromptTokenCount: 1, CandidatesTokenCount: 1, TotalTokenCount: 2}
	f.TraceID = "refcapture"
	return f
}

// writeFrame emits one SSE frame.
func writeFrame(w http.ResponseWriter, flusher http.Flusher, f frame) {
	b, err := json.Marshal(f)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", b)
	if flusher != nil {
		flusher.Flush()
	}
}
