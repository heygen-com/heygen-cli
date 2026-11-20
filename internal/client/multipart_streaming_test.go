package client

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"runtime"
	"testing"

	"github.com/heygen-com/heygen-cli/internal/command"
)

// bounded-alloc check — stream must not grow RSS with file size.
func TestMultipartStreaming_BoundedMemory(t *testing.T) {
	const mb = 1 << 20
	var peak uint64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		peak = m.TotalAlloc
		_, _ = w.Write([]byte(`{"data":{}}`))
	}))
	defer srv.Close()

	f, err := os.CreateTemp("", "stream-*.bin")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	chunk := make([]byte, mb)
	if _, err := f.Write(chunk); err != nil {
		t.Fatal(err)
	}
	f.Close()

	c := New("key", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
	spec := &command.Spec{Endpoint: "/upload", Method: "POST", BodyEncoding: "multipart"}
	inv := &command.Invocation{PathParams: map[string]string{}, QueryParams: url.Values{}, FilePath: f.Name()}
	if _, err := c.Execute(spec, inv); err != nil {
		t.Fatal(err)
	}
	if peak > 16*mb {
		t.Fatalf("allocated %d MB streaming a 1 MB file; want <16 MB (buffer copy)", peak/mb)
	}
	fmt.Fprintf(os.Stderr, "streamed 1 MB, alloc=%d KB\n", peak>>10)
}
