package proxy

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/tidwall/gjson"

	"github.com/sobakintech/maitake/internal/translate"
)

const maxImageUpload = 32 << 20

// Tool options passed through from the Images API request, with their JSON types.
var (
	imageStringOpts = []string{"size", "quality", "background", "output_format", "moderation", "input_fidelity"}
	imageIntOpts    = []string{"output_compression", "partial_images"}
)

type imageRequest struct {
	prompt  string
	model   string
	opts    map[string]any
	images  []string // data or https URLs
	mask    string
	asURL   bool
	stream  bool
	editing bool
}

func (q *imageRequest) eventType() string {
	if q.editing {
		return "image_edit.completed"
	}
	return "image_generation.completed"
}

// serveImages implements /v1/images/generations and /v1/images/edits on top of Codex's
// image_generation tool: a Responses call that forces the tool, with the result unwrapped.
func (h *Handler) serveImages(w http.ResponseWriter, r *http.Request) {
	f := formatResponses
	rs := h.routing()
	if !rs.Images.Enabled {
		writeError(w, f, newError(http.StatusNotFound, "image generation is disabled"))
		return
	}
	owner := h.authenticate(w, r, f)
	if owner == nil {
		return
	}
	req, err := parseImageRequest(w, r)
	if err != nil {
		writeError(w, f, newError(http.StatusBadRequest, err.Error()))
		return
	}
	if strings.TrimSpace(req.prompt) == "" {
		writeError(w, f, newError(http.StatusBadRequest, "prompt is required"))
		return
	}
	if req.editing && len(req.images) == 0 {
		writeError(w, f, newError(http.StatusBadRequest, "image is required"))
		return
	}
	toolModel := req.model
	if !strings.HasPrefix(strings.ToLower(toolModel), "gpt-image") {
		toolModel = rs.Images.ToolModel
	}

	tool := map[string]any{"type": "image_generation", "action": "generate", "model": toolModel}
	if req.editing {
		tool["action"] = "edit"
	}
	for k, v := range req.opts {
		tool[k] = v
	}
	if req.mask != "" {
		tool["input_image_mask"] = map[string]string{"image_url": req.mask}
	}
	content := []map[string]any{{"type": "input_text", "text": req.prompt}}
	for _, img := range req.images {
		content = append(content, map[string]any{"type": "input_image", "image_url": img})
	}
	body, _ := json.Marshal(map[string]any{
		"instructions": "", "stream": true, "store": false, "model": rs.Images.Model,
		"reasoning": map[string]string{"effort": "medium", "summary": "auto"}, "parallel_tool_calls": true,
		"include": []string{"reasoning.encrypted_content"}, "tool_choice": map[string]string{"type": "image_generation"},
		"tools": []any{tool}, "input": []any{map[string]any{"type": "message", "role": "user", "content": content}},
	})

	c := &call{format: f, body: body, kind: kindImages, owner: owner, model: rs.Images.Model, display: toolModel}
	c.respond = func(w http.ResponseWriter, response []byte, u translate.Usage) relayResult {
		return writeImages(w, response, req, u)
	}
	h.handle(w, r, c, false)
}

func parseImageRequest(w http.ResponseWriter, r *http.Request) (*imageRequest, error) {
	req := &imageRequest{opts: map[string]any{}, editing: strings.HasSuffix(r.URL.Path, "/edits")}
	if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt == "multipart/form-data" {
		return req, parseImageForm(w, r, req)
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxImageUpload))
	if err != nil {
		return nil, fmt.Errorf("request body too large or unreadable")
	}
	if !gjson.ValidBytes(body) {
		return nil, fmt.Errorf("request body must be valid JSON or multipart form data")
	}
	j := gjson.ParseBytes(body)
	req.prompt, req.model = j.Get("prompt").String(), j.Get("model").String()
	req.asURL = j.Get("response_format").String() == "url"
	req.stream = j.Get("stream").Bool()
	for _, k := range imageStringOpts {
		if v := j.Get(k); v.Type == gjson.String && v.String() != "" {
			req.opts[k] = v.String()
		}
	}
	for _, k := range imageIntOpts {
		if v := j.Get(k); v.Type == gjson.Number {
			req.opts[k] = v.Int()
		}
	}
	imageURL := func(v gjson.Result) string {
		if v.Type == gjson.String {
			return v.String()
		}
		return v.Get("image_url").String()
	}
	for _, field := range []string{"images", "image"} {
		v := j.Get(field)
		if v.IsArray() {
			for _, it := range v.Array() {
				if u := imageURL(it); u != "" {
					req.images = append(req.images, u)
				}
			}
		} else if u := imageURL(v); u != "" {
			req.images = append(req.images, u)
		}
	}
	req.mask = imageURL(j.Get("mask"))
	return req, nil
}

func parseImageForm(w http.ResponseWriter, r *http.Request, req *imageRequest) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxImageUpload)
	if err := r.ParseMultipartForm(maxImageUpload); err != nil {
		return fmt.Errorf("invalid multipart body: %v", err)
	}
	form := r.MultipartForm
	get := func(k string) string {
		if v := form.Value[k]; len(v) > 0 {
			return v[0]
		}
		return ""
	}
	req.prompt, req.model = get("prompt"), get("model")
	req.asURL = get("response_format") == "url"
	req.stream = get("stream") == "true"
	for _, k := range imageStringOpts {
		if v := get(k); v != "" {
			req.opts[k] = v
		}
	}
	for _, k := range imageIntOpts {
		if n, err := strconv.Atoi(get(k)); err == nil {
			req.opts[k] = n
		}
	}
	for _, field := range []string{"image[]", "image"} {
		for _, fh := range form.File[field] {
			url, err := fileDataURL(fh)
			if err != nil {
				return err
			}
			req.images = append(req.images, url)
		}
	}
	if fhs := form.File["mask"]; len(fhs) > 0 {
		url, err := fileDataURL(fhs[0])
		if err != nil {
			return err
		}
		req.mask = url
	}
	return nil
}

func fileDataURL(fh *multipart.FileHeader) (string, error) {
	f, err := fh.Open()
	if err != nil {
		return "", err
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		return "", err
	}
	ct := fh.Header.Get("Content-Type")
	if ct == "" || ct == "application/octet-stream" {
		ct = http.DetectContentType(data)
	}
	return "data:" + ct + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

// writeImages turns the finished Responses object into an Images API response (or its
// completion events when the client asked for a stream).
func writeImages(w http.ResponseWriter, response []byte, req *imageRequest, u translate.Usage) relayResult {
	res := gjson.ParseBytes(response)
	var calls []gjson.Result
	res.Get("output").ForEach(func(_, it gjson.Result) bool {
		if it.Get("type").String() == "image_generation_call" && it.Get("result").String() != "" {
			calls = append(calls, it)
		}
		return true
	})
	if len(calls) == 0 {
		msg := "upstream returned no image"
		if t := strings.TrimSpace(outputText(res)); t != "" {
			msg += ": " + t
		}
		e := newError(http.StatusBadGateway, msg)
		writeError(w, formatResponses, e)
		return relayResult{status: e.status, usage: u, msg: msg}
	}
	created := time.Now().Unix()
	usage := res.Get("tool_usage.image_gen")
	format := calls[0].Get("output_format").String()
	if format == "" {
		format = "png"
	}
	meta := func(dst map[string]any, it gjson.Result) {
		for _, k := range []string{"size", "quality", "background"} {
			if v := it.Get(k).String(); v != "" {
				dst[k] = v
			}
		}
		if usage.Exists() {
			dst["usage"] = json.RawMessage(usage.Raw)
		}
	}

	if req.stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		for _, it := range calls {
			ev := map[string]any{"type": req.eventType(), "b64_json": it.Get("result").String(),
				"created_at": created, "output_format": format}
			meta(ev, it)
			data, _ := json.Marshal(ev)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", req.eventType(), data)
		}
		if fl, ok := w.(http.Flusher); ok {
			fl.Flush()
		}
		return relayResult{status: http.StatusOK, usage: u}
	}

	out := map[string]any{"created": created, "output_format": format}
	meta(out, calls[0])
	data := make([]map[string]any, 0, len(calls))
	for _, it := range calls {
		item := map[string]any{}
		if req.asURL {
			item["url"] = "data:image/" + format + ";base64," + it.Get("result").String()
		} else {
			item["b64_json"] = it.Get("result").String()
		}
		if p := it.Get("revised_prompt").String(); p != "" {
			item["revised_prompt"] = p
		}
		data = append(data, item)
	}
	out["data"] = data
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(out)
	return relayResult{status: http.StatusOK, usage: u}
}

func outputText(res gjson.Result) string {
	var b strings.Builder
	res.Get("output").ForEach(func(_, it gjson.Result) bool {
		if it.Get("type").String() == "message" {
			it.Get("content").ForEach(func(_, c gjson.Result) bool {
				b.WriteString(c.Get("text").String())
				return true
			})
		}
		return true
	})
	return b.String()
}
