package agent

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Autumn-27/norma/llm"
	"github.com/Autumn-27/norma/transcript"
)

// The SDK's Resume reads an entire transcript. Raw main files are private to the
// trusted API; workers cannot mount them and workspace mutations forbid them.
// The conversation lock serializes API writers, so a stat ceiling before the one
// SDK read bounds resume allocations without reading/parsing the file twice.
const maxChatRawResumeBytes int64 = 4 << 20
const maxChatRawSeedBytes int64 = 256 << 10

func chatRawTraceInfo(info fs.FileInfo, err error) (bool, error) {
	if os.IsNotExist(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return false, errors.New("raw session trace must be an ordinary file without symlinks")
	}
	if info.Size() > maxChatRawResumeBytes {
		return false, errors.New("raw session trace exceeds the 4 MiB resume ceiling; archive or remove it and retry using bounded PostgreSQL history")
	}
	return info.Size() == 0, nil
}

// Missing/empty raw files need a durable-in-this-tmpfs seed, not just a provider
// prefix: otherwise the next turn would resume only the newly recorded turn and
// silently discard the older PostgreSQL context. SDK Writer supplies the exact
// record schema, UUID/parent chain and serialization. A private staging directory
// makes the bounded seed visible in one same-filesystem rename after fsync.
func prepareChatRawResume(store *transcript.Store, sessionID string, history []llm.Message) (bool, error) {
	if sessionID == "" || strings.IndexFunc(sessionID, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_')
	}) >= 0 {
		return false, errors.New("invalid raw conversation session identity")
	}
	name := store.MainPath(sessionID)
	empty, err := chatRawTraceInfo(os.Lstat(name))
	if err != nil {
		return false, fmt.Errorf("cannot inspect raw conversation trace: %w", err)
	}
	if !empty {
		return true, nil
	}
	history = boundedChatHistory(history, ChatHistoryMaxBytes)
	if len(history) == 0 {
		return false, nil
	}
	directory := filepath.Dir(name)
	if err := os.MkdirAll(directory, 0700); err != nil {
		return false, fmt.Errorf("cannot create ephemeral conversation trace directory: %w", err)
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return false, fmt.Errorf("cannot open ephemeral conversation trace directory: %w", err)
	}
	defer root.Close()
	temporary := ".history-seed-" + transcript.NewSessionID()
	if err = root.Mkdir(temporary, 0700); err != nil {
		return false, fmt.Errorf("cannot stage bounded conversation history: %w", err)
	}
	defer root.RemoveAll(temporary)
	seed := transcript.NewStore(filepath.Join(directory, temporary))
	writer := seed.NewWriter(sessionID, "")
	for _, message := range history {
		writer.RecordMessage(message, llm.Usage{})
		if err = writer.Err(); err != nil {
			return false, fmt.Errorf("cannot preserve bounded conversation history seed; free ephemeral trace capacity and retry: %w", err)
		}
	}
	staged := filepath.Join(temporary, filepath.Base(name))
	file, err := root.OpenFile(staged, os.O_RDWR, 0)
	if err != nil {
		return false, fmt.Errorf("cannot open bounded conversation history seed: %w", err)
	}
	info, statErr := file.Stat()
	if statErr != nil || !info.Mode().IsRegular() || info.Size() > maxChatRawSeedBytes {
		file.Close()
		return false, errors.New("bounded conversation history seed exceeded its serialization budget")
	}
	if err = errors.Join(file.Sync(), file.Close()); err != nil {
		return false, fmt.Errorf("cannot sync bounded conversation history seed: %w", err)
	}
	// Check again before committing, preserving any valid trace that appeared
	// during staging. The supported single API serializes this conversation.
	empty, err = chatRawTraceInfo(root.Lstat(filepath.Base(name)))
	if err != nil {
		return false, fmt.Errorf("raw conversation trace changed during history seeding: %w", err)
	}
	if !empty {
		return true, nil
	}
	if err = root.Rename(staged, filepath.Base(name)); err != nil {
		return false, fmt.Errorf("cannot commit bounded conversation history seed: %w", err)
	}
	return true, nil
}
