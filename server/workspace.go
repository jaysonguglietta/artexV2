package server

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

// Workspace file manager — browse / view / edit / download / upload / delete the
// shared work dir (s.m.dir), where all agents write their artifacts. Every path is
// confined to the work dir root (traversal via ".." is neutralised). All routes sit
// behind requireAuth (see Handler()).

const (
	maxWorkspaceRead   = 2 << 20   // 2 MiB: files bigger than this aren't inlined for view/edit (download instead)
	maxWorkspaceUpload = 512 << 20 // 512 MiB per upload request
)

// User-facing workspace errors use English while retaining requested paths and original diagnostic details.
const (
	errWsIllegalPath      = "Invalid path"
	errWsPathNotFound     = "The path does not exist"
	errWsNotDir           = "Not a directory"
	errWsFileNotFound     = "The file does not exist"
	errWsIsDir            = "It is a directory, so it cannot be read as a file"
	errWsTargetIsDir      = "The target path is a directory"
	errWsCannotDeleteRoot = "The workspace root directory cannot be deleted"
	errWsUploadDirMissing = "The target directory does not exist"
	errWsUploadParse      = "Upload processing failed or exceeded its size limit: "
	errWsNoUploadFile     = "There is no file to upload (form field file)"
)

// wsResolve maps a user-supplied relative path to an absolute path INSIDE the work
// dir. It returns ok=false if the path would escape the root. filepath.Clean on a
// rooted copy collapses any ".." so nothing can climb above the root.
func (s *Server) wsResolve(rel string) (string, bool) {
	base := filepath.Clean(s.m.dir)
	rel = strings.TrimPrefix(strings.TrimSpace(rel), "/")
	clean := filepath.Clean("/" + rel) // e.g. "/a/../../etc" → "/etc" (still rooted at "/")
	abs := filepath.Clean(filepath.Join(base, clean))
	if abs != base && !strings.HasPrefix(abs, base+string(os.PathSeparator)) {
		return "", false
	}
	return abs, true
}

// wsRel renders an absolute path back as a workspace-relative path (forward slashes).
func (s *Server) wsRel(abs string) string {
	base := filepath.Clean(s.m.dir)
	rel, err := filepath.Rel(base, abs)
	if err != nil || rel == "." {
		return ""
	}
	return filepath.ToSlash(rel)
}

type wsEntry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	Dir   bool   `json:"dir"`
	Size  int64  `json:"size"`
	MTime int64  `json:"mtime"` // unix millis
}

// GET /api/workspace/list?path=<rel>
func (s *Server) wsList(w http.ResponseWriter, r *http.Request) {
	root, err := os.OpenRoot(s.m.dir)
	if err != nil {
		writeErr(w, 500, "Workspace unavailable")
		return
	}
	defer root.Close()
	relPath := func(abs string) string { rel, _ := filepath.Rel(s.m.dir, abs); return rel }
	_ = relPath

	abs, ok := s.wsResolve(r.URL.Query().Get("path"))
	if !ok {
		writeErr(w, 400, errWsIllegalPath)
		return
	}
	fi, err := root.Stat(relPath(abs))
	if err != nil {
		writeErr(w, 404, errWsPathNotFound)
		return
	}
	if !fi.IsDir() {
		writeErr(w, 400, errWsNotDir)
		return
	}
	dir, err := root.Open(relPath(abs))
	if err != nil {
		writeErr(w, 400, errWsIllegalPath)
		return
	}
	defer dir.Close()
	ents, err := dir.ReadDir(-1)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	out := make([]wsEntry, 0, len(ents))
	for _, e := range ents {
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, wsEntry{
			Name:  e.Name(),
			Path:  s.wsRel(filepath.Join(abs, e.Name())),
			Dir:   e.IsDir(),
			Size:  info.Size(),
			MTime: info.ModTime().UnixMilli(),
		})
	}
	// Sort directories first, then sort names within each group.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Dir != out[j].Dir {
			return out[i].Dir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	writeJSON(w, 200, map[string]any{"path": s.wsRel(abs), "entries": out})
}

// GET /api/workspace/read?path=<rel> — inline text for view/edit. Binary or oversize
// files return {binary:true}/{too_large:true} with no content (use download instead).
func (s *Server) wsRead(w http.ResponseWriter, r *http.Request) {
	root, err := os.OpenRoot(s.m.dir)
	if err != nil {
		writeErr(w, 500, "Workspace unavailable")
		return
	}
	defer root.Close()
	relPath := func(abs string) string { rel, _ := filepath.Rel(s.m.dir, abs); return rel }
	_ = relPath

	abs, ok := s.wsResolve(r.URL.Query().Get("path"))
	if !ok {
		writeErr(w, 400, errWsIllegalPath)
		return
	}
	fi, err := root.Stat(relPath(abs))
	if err != nil {
		writeErr(w, 404, errWsFileNotFound)
		return
	}
	if fi.IsDir() {
		writeErr(w, 400, errWsIsDir)
		return
	}
	if fi.Size() > maxWorkspaceRead {
		writeJSON(w, 200, map[string]any{"path": s.wsRel(abs), "size": fi.Size(), "too_large": true, "binary": true})
		return
	}
	if !fi.Mode().IsRegular() {
		writeErr(w, 400, errWsIllegalPath)
		return
	}
	f, err := root.Open(relPath(abs))
	if err != nil {
		writeErr(w, 400, errWsIllegalPath)
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxWorkspaceRead+1))
	if len(data) > maxWorkspaceRead {
		writeErr(w, 413, "File exceeds read limit")
		return
	}
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
		writeJSON(w, 200, map[string]any{"path": s.wsRel(abs), "size": fi.Size(), "binary": true})
		return
	}
	writeJSON(w, 200, map[string]any{"path": s.wsRel(abs), "size": fi.Size(), "binary": false, "content": string(data)})
}

// POST /api/workspace/write  {path, content} — create/overwrite a text file.
func (s *Server) wsWrite(w http.ResponseWriter, r *http.Request) {
	root, err := os.OpenRoot(s.m.dir)
	if err != nil {
		writeErr(w, 500, "Workspace unavailable")
		return
	}
	defer root.Close()
	relPath := func(abs string) string { rel, _ := filepath.Rel(s.m.dir, abs); return rel }
	_ = relPath

	var req struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	abs, ok := s.wsResolve(req.Path)
	if !ok || abs == filepath.Clean(s.m.dir) {
		writeErr(w, 400, errWsIllegalPath)
		return
	}
	if fi, err := root.Stat(relPath(abs)); err == nil && fi.IsDir() {
		writeErr(w, 400, errWsTargetIsDir)
		return
	}
	if err := root.MkdirAll(filepath.Dir(relPath(abs)), 0o700); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if fi, err := root.Stat(relPath(abs)); err == nil && !fi.Mode().IsRegular() {
		writeErr(w, 400, errWsIllegalPath)
		return
	}
	if err := root.WriteFile(relPath(abs), []byte(req.Content), 0o600); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "path": s.wsRel(abs)})
}

// POST /api/workspace/mkdir  {path}
func (s *Server) wsMkdir(w http.ResponseWriter, r *http.Request) {
	root, err := os.OpenRoot(s.m.dir)
	if err != nil {
		writeErr(w, 500, "Workspace unavailable")
		return
	}
	defer root.Close()
	relPath := func(abs string) string { rel, _ := filepath.Rel(s.m.dir, abs); return rel }
	_ = relPath

	var req struct {
		Path string `json:"path"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	abs, ok := s.wsResolve(req.Path)
	if !ok || abs == filepath.Clean(s.m.dir) {
		writeErr(w, 400, errWsIllegalPath)
		return
	}
	if err := root.MkdirAll(relPath(abs), 0o700); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "path": s.wsRel(abs)})
}

// DELETE /api/workspace/delete?path=<rel> — removes a file or a directory tree
// (confined to the work dir; the root itself can't be deleted).
func (s *Server) wsDelete(w http.ResponseWriter, r *http.Request) {
	root, err := os.OpenRoot(s.m.dir)
	if err != nil {
		writeErr(w, 500, "Workspace unavailable")
		return
	}
	defer root.Close()
	relPath := func(abs string) string { rel, _ := filepath.Rel(s.m.dir, abs); return rel }
	_ = relPath

	abs, ok := s.wsResolve(r.URL.Query().Get("path"))
	if !ok {
		writeErr(w, 400, errWsIllegalPath)
		return
	}
	if abs == filepath.Clean(s.m.dir) {
		writeErr(w, 400, errWsCannotDeleteRoot)
		return
	}
	if _, err := root.Stat(relPath(abs)); err != nil {
		writeErr(w, 404, errWsPathNotFound)
		return
	}
	if err := root.RemoveAll(relPath(abs)); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// GET /api/workspace/download?path=<rel> — stream a file as an attachment.
func (s *Server) wsDownload(w http.ResponseWriter, r *http.Request) {
	root, err := os.OpenRoot(s.m.dir)
	if err != nil {
		writeErr(w, 500, "Workspace unavailable")
		return
	}
	defer root.Close()
	relPath := func(abs string) string { rel, _ := filepath.Rel(s.m.dir, abs); return rel }
	_ = relPath

	abs, ok := s.wsResolve(r.URL.Query().Get("path"))
	if !ok {
		writeErr(w, 400, errWsIllegalPath)
		return
	}
	fi, err := root.Stat(relPath(abs))
	if err != nil || fi.IsDir() {
		writeErr(w, 404, errWsFileNotFound)
		return
	}
	name := filepath.Base(abs)
	// RFC 5987 filename* keeps non-ASCII names intact; plain filename is the fallback.
	w.Header().Set("Content-Disposition", "attachment; filename=\""+sanitizeFilename(name)+"\"; filename*=UTF-8''"+url.PathEscape(name))
	if !fi.Mode().IsRegular() {
		writeErr(w, 400, errWsIllegalPath)
		return
	}
	f, err := root.Open(relPath(abs))
	if err != nil {
		writeErr(w, 400, errWsIllegalPath)
		return
	}
	defer f.Close()
	http.ServeContent(w, r, name, fi.ModTime(), f)
}

// POST /api/workspace/upload?path=<dir> — multipart form field "file" (one or more).
func (s *Server) wsUpload(w http.ResponseWriter, r *http.Request) {
	root, err := os.OpenRoot(s.m.dir)
	if err != nil {
		writeErr(w, 500, "Workspace unavailable")
		return
	}
	defer root.Close()
	relPath := func(abs string) string { rel, _ := filepath.Rel(s.m.dir, abs); return rel }
	_ = relPath

	dirAbs, ok := s.wsResolve(r.URL.Query().Get("path"))
	if !ok {
		writeErr(w, 400, errWsIllegalPath)
		return
	}
	if fi, err := root.Stat(relPath(dirAbs)); err != nil || !fi.IsDir() {
		writeErr(w, 400, errWsUploadDirMissing)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxWorkspaceUpload)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeErr(w, 400, errWsUploadParse+err.Error())
		return
	}
	files := r.MultipartForm.File["file"]
	if len(files) == 0 {
		writeErr(w, 400, errWsNoUploadFile)
		return
	}
	saved := 0
	for _, hdr := range files {
		name := filepath.Base(hdr.Filename) // strip any path component
		if name == "" || name == "." || name == ".." {
			continue
		}
		destAbs, okd := s.wsResolve(filepath.Join(s.wsRel(dirAbs), name))
		if !okd {
			continue
		}
		if fi, err := root.Stat(relPath(destAbs)); err == nil && !fi.Mode().IsRegular() {
			writeErr(w, 400, errWsIllegalPath)
			return
		}
		if err := saveRootUpload(root, hdr, relPath(destAbs)); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		saved++
	}
	writeJSON(w, 200, map[string]any{"uploaded": saved})
}

func saveUpload(hdr *multipart.FileHeader, dest string) error {
	src, err := hdr.Open()
	if err != nil {
		return err
	}
	defer src.Close()
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, src)
	return err
}

// sanitizeFilename strips characters unsafe for a Content-Disposition filename token.
func sanitizeFilename(name string) string {
	name = strings.ReplaceAll(name, "\"", "")
	name = strings.ReplaceAll(name, "\\", "")
	name = strings.ReplaceAll(name, "\n", "")
	name = strings.ReplaceAll(name, "\r", "")
	return name
}

// saveRootUpload opens through a directory handle rather than following absolute paths.
func saveRootUpload(root *os.Root, hdr *multipart.FileHeader, dest string) error {
	src, err := hdr.Open()
	if err != nil {
		return err
	}
	defer src.Close()
	out, err := root.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, src)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}
