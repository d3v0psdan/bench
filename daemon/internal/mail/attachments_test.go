package mail

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveAttachmentWritesADownloadWithoutOverwriting(t *testing.T) {
	mailpit := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/message/abc":
			w.Write([]byte(`{"Attachments":[{"PartID":"2","FileName":"../../invoice.pdf"}]}`))
		case "/api/v1/message/abc/part/2":
			w.Write([]byte("PDF bytes"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer mailpit.Close()
	target := Target{Addr: strings.TrimPrefix(mailpit.URL, "http://")}
	dir := t.TempDir()
	h := SaveAttachment(func() (Target, bool) { return target, true }, func() (string, error) { return dir, nil })

	for _, want := range []string{"invoice.pdf", "invoice (2).pdf"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("POST", "/api/mail/save-attachment", strings.NewReader(`{"id":"abc","part":"2"}`)))
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d: %s", rec.Code, rec.Body)
		}
		b, err := os.ReadFile(filepath.Join(dir, want))
		if err != nil || string(b) != "PDF bytes" {
			t.Fatalf("%s: %q, %v (the sender's ../ must not escape the folder)", want, b, err)
		}
	}

	for _, body := range []string{`{"id":"../x","part":"2"}`, `{"id":"abc","part":"2/../../3"}`, `{"id":"abc","part":"9"}`} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("POST", "/api/mail/save-attachment", strings.NewReader(body)))
		if rec.Code == http.StatusOK {
			t.Errorf("%s: saved something, want a refusal", body)
		}
	}
}

func TestSafeNameKeepsNamesHonest(t *testing.T) {
	for in, want := range map[string]string{
		"invoice\u202efdp.exe": "invoicefdp.exe",
		"a.exe. ":              "a.exe",
		"CON":                  "_CON",
		"aux.txt":              "_aux.txt",
		"../../x.pdf":          "x.pdf",
		"report.pdf":           "report.pdf",
	} {
		if got := safeName(in); got != want {
			t.Errorf("safeName(%q) = %q, want %q", in, got, want)
		}
	}
}
