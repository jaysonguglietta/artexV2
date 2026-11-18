package server

import (
	"errors"
	"fmt"
	"github.com/Autumn-27/artex/storage"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Multipart bodies are streamed directly into admitted private staging. The
// stdlib ReadForm spill directory is deliberately unused: global /tmp is outside
// the persistent workspace quota in ordinary binary deployments.
func (s *Server) streamUploads(w http.ResponseWriter, r *http.Request, directory string, limit int64) ([]chatAttachment, error) {
	if storage.ForPath(s.m.dir) == nil {
		if _, err := storage.ConfigureDataDir(s.m.dir); err != nil {
			return nil, err
		}
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	reader, err := r.MultipartReader()
	if err != nil {
		return nil, err
	}
	stage, err := storage.TempDir(filepath.Join(s.m.dir, "_upload-staging"), "request-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stage)
	type staged struct {
		name, path string
		size       int64
	}
	var files []staged
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if part.FormName() != "file" || part.FileName() == "" {
			if err = part.Close(); err != nil {
				return nil, err
			}
			continue
		}
		name := filepath.Base(part.FileName())
		if name == "." || name == ".." || len(name) > 255 || strings.ContainsAny(name, "/\\\x00") {
			part.Close()
			return nil, errors.New("invalid upload filename")
		}
		if err := s.workspaceMutationAllowed(filepath.Join(directory, name)); err != nil {
			part.Close()
			return nil, err
		}
		if len(files) >= 1000 {
			part.Close()
			return nil, errors.New("upload request exceeds 1000 files")
		}
		path := filepath.Join(stage, fmt.Sprintf("part-%d", len(files)))
		file, err := storage.CreateFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			part.Close()
			return nil, err
		}
		size, copyErr := io.Copy(file, part)
		syncErr := file.Sync()
		closeErr := file.Close()
		partErr := part.Close()
		if err = errors.Join(copyErr, syncErr, closeErr, partErr); err != nil {
			return nil, err
		}
		files = append(files, staged{name, path, size})
	}
	if len(files) == 0 {
		return nil, errors.New(errChatUploadNoFile)
	}
	out := make([]chatAttachment, 0, len(files))
	for _, file := range files {
		target, err := storage.MoveUnique(file.path, directory, file.name)
		if err != nil {
			return nil, err
		}
		name := filepath.Base(target)
		out = append(out, chatAttachment{Name: name, Path: "uploads/" + name, Abs: target, Size: file.size})
	}
	return out, nil
}
