package provider

import (
	"compress/flate"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
)

// Decompress replaces resp.Body with a decoding reader. Needed because we set Accept-Encoding
// ourselves to match real clients, which disables Go's transparent gzip handling.
func Decompress(resp *http.Response) error {
	enc := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Encoding")))
	var r io.Reader
	switch enc {
	case "", "identity":
		return nil
	case "gzip":
		gz, err := gzip.NewReader(resp.Body)
		if err != nil {
			return err
		}
		r = gz
	case "deflate":
		r = flate.NewReader(resp.Body)
	case "br":
		r = brotli.NewReader(resp.Body)
	case "zstd":
		zr, err := zstd.NewReader(resp.Body)
		if err != nil {
			return err
		}
		r = zr.IOReadCloser()
	default:
		return fmt.Errorf("unsupported content encoding %q", enc)
	}
	resp.Body = readCloser{r, resp.Body}
	resp.Header.Del("Content-Encoding")
	resp.Header.Del("Content-Length")
	resp.ContentLength = -1
	return nil
}

type readCloser struct {
	io.Reader
	body io.Closer
}

func (rc readCloser) Close() error {
	if c, ok := rc.Reader.(io.Closer); ok {
		c.Close()
	}
	return rc.body.Close()
}
