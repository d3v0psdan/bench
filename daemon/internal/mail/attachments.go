package mail

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"unicode"
)

// maxRequestBytes bounds the {"id","part"} body.
const maxRequestBytes = 4 << 10

// internetZone is the Zone.Identifier stream browsers write on downloads.
const internetZone = "[ZoneTransfer]\r\nZoneId=3\r\n"

// maxCopies is how many "name (n).ext" copies are tried before giving up.
const maxCopies = 1000

var (
	messageIDRe = regexp.MustCompile(`^[A-Za-z0-9-]+$`)
	partIDRe    = regexp.MustCompile(`^[0-9]+(\.[0-9]+)*$`)
)

// SaveAttachment serves POST /api/mail/save-attachment {"id","part"}: it
// copies one attachment from Mailpit into dir() (the user's Downloads) and
// answers {"path"}, so the GUI can show it in its folder. The webview has
// no file access of its own; the daemon, as the user, does.
func SaveAttachment(find Finder, dir func() (string, error)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID   string `json:"id"`
			Part string `json:"part"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBytes)).Decode(&req); err != nil ||
			!messageIDRe.MatchString(req.ID) || !partIDRe.MatchString(req.Part) {
			http.Error(w, "need a message id and an attachment part", http.StatusBadRequest)
			return
		}
		t, ok := find()
		if !ok {
			http.Error(w, "mail is not running", http.StatusServiceUnavailable)
			return
		}
		path, err := saveAttachment(r, t, req.ID, req.Part, dir)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]string{"path": path}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})
}

func saveAttachment(r *http.Request, t Target, id, part string, dir func() (string, error)) (string, error) {
	var msg struct {
		Attachments []struct {
			PartID   string
			FileName string
		}
	}
	if err := t.getJSON(r, "/api/v1/message/"+id, &msg); err != nil {
		return "", fmt.Errorf("reading message %s: %w", id, err)
	}
	name := ""
	for _, a := range msg.Attachments {
		if a.PartID == part {
			name = a.FileName
		}
	}
	if name == "" {
		return "", fmt.Errorf("message %s has no attachment %s", id, part)
	}
	resp, err := t.get(r, "/api/v1/message/"+id+"/part/"+part)
	if err != nil {
		return "", fmt.Errorf("downloading %s: %w", name, err)
	}
	defer resp.Body.Close()

	folder, err := dir()
	if err != nil {
		return "", fmt.Errorf("finding the Downloads folder: %w", err)
	}
	if err := os.MkdirAll(folder, 0o755); err != nil {
		return "", fmt.Errorf("creating %s: %w", folder, err)
	}
	f, path, err := createUnique(folder, safeName(name))
	if err != nil {
		return "", fmt.Errorf("saving %s: %w", name, err)
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(path)
		return "", fmt.Errorf("saving %s: %w", name, err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("saving %s: %w", name, err)
	}
	markDownloaded(path)
	return path, nil
}

// markDownloaded tags a saved attachment as coming from outside (Mark of
// the Web on Windows), so SmartScreen and Office's Protected View treat it
// like any download. Best effort: a volume without streams just skips it.
func markDownloaded(path string) {
	if runtime.GOOS != "windows" {
		return
	}
	_ = os.WriteFile(path+":Zone.Identifier", []byte(internetZone), 0o644)
}

// safeName keeps a sender's file name to one plain, honest file name: no
// folders, nothing that climbs out of the target folder, no invisible
// direction marks (U+202E turns "fdp.exe" into "exe.pdf" on screen), no
// trailing dots or spaces Windows drops ("a.exe." is "a.exe"), and no
// device names Windows opens instead of a file (CON, NUL, AUX.txt).
func safeName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, `\`, "/"))
	name = strings.Map(func(r rune) rune {
		switch {
		case strings.ContainsRune(`<>:"/\|?*`, r) || r < 32:
			return '_'
		case unicode.Is(unicode.Cf, r):
			return -1
		}
		return r
	}, name)
	name = strings.TrimRight(name, ". ")
	if name == "" || name == "." || name == ".." {
		return "attachment"
	}
	stem := strings.ToUpper(strings.TrimSuffix(name, filepath.Ext(name)))
	if reservedNames[stem] {
		return "_" + name
	}
	return name
}

// reservedNames are Windows device names, with or without an extension.
var reservedNames = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true, "COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true, "LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}

// createUnique creates name in dir, or "name (2).ext" and so on when it
// exists, never overwriting a file.
func createUnique(dir, name string) (*os.File, string, error) {
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i := 1; i < maxCopies; i++ {
		candidate := name
		if i > 1 {
			candidate = fmt.Sprintf("%s (%d)%s", base, i, ext)
		}
		path := filepath.Join(dir, candidate)
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		return f, path, err
	}
	return nil, "", fmt.Errorf("too many copies of %s in %s", name, dir)
}

func (t Target) get(r *http.Request, path string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, "http://"+t.Addr+path, nil)
	if err != nil {
		return nil, err
	}
	if t.Auth != "" {
		req.Header.Set("Authorization", t.authHeader())
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("asking mail: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("mail answered %s for %s", resp.Status, path)
	}
	return resp, nil
}

func (t Target) getJSON(r *http.Request, path string, v any) error {
	resp, err := t.get(r, path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(v)
}
