package plugin

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

const maxStorageBytes = 1 << 20

var storageKey = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

type Store struct{ Directory string }

func (store *Store) Call(ctx context.Context, id, method string, params json.RawMessage) Reply {
	fail := func(code, message string) Reply { return Reply{Error: problem(code, message)} }
	var request struct {
		Key   string          `json:"key"`
		Value json.RawMessage `json:"value"`
	}
	if !validID(id) || json.Unmarshal(params, &request) != nil || !storageKey.MatchString(request.Key) {
		return fail("invalid_params", "storage requires an alphanumeric key of 1 to 64 characters")
	}
	if method != "storage.get" && method != "storage.set" && method != "storage.delete" {
		return fail("unknown_method", "unsupported storage method")
	}
	if method == "storage.set" && (len(request.Value) == 0 || len(request.Value) > 32*1024) {
		return fail("too_large", "storage values must be valid JSON of at most 32 KiB")
	}
	if ctx.Err() != nil {
		return fail("canceled", "storage operation canceled")
	}
	directory := filepath.Join(store.Directory, id)
	if os.MkdirAll(directory, 0o700) != nil {
		return fail("storage_error", "cannot create plugin storage")
	}
	lock, err := os.OpenFile(filepath.Join(directory, ".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fail("storage_error", "cannot open storage lock")
	}
	defer lock.Close()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		locked, err := tryStorageLock(lock)
		if err != nil {
			return fail("storage_error", "cannot lock plugin storage")
		}
		if locked {
			break
		}
		select {
		case <-ctx.Done():
			return fail("canceled", "storage operation canceled")
		case <-ticker.C:
		}
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return fail("storage_error", "cannot open plugin storage")
	}
	defer root.Close()
	values := map[string]json.RawMessage{}
	if info, err := root.Lstat("values.json"); err == nil && !info.Mode().IsRegular() {
		return fail("storage_error", "plugin storage must be a regular file")
	} else if err != nil && !os.IsNotExist(err) {
		return fail("storage_error", "cannot inspect plugin storage")
	}
	file, err := root.Open("values.json")
	if err == nil {
		data, readErr := io.ReadAll(io.LimitReader(file, maxStorageBytes+1))
		file.Close()
		if readErr != nil || len(data) > maxStorageBytes || json.Unmarshal(data, &values) != nil || values == nil {
			return fail("storage_error", "invalid plugin storage")
		}
	} else if !os.IsNotExist(err) {
		return fail("storage_error", "cannot read plugin storage")
	}
	if method == "storage.get" {
		return Reply{Result: map[string]any{"value": values[request.Key]}}
	}
	if method == "storage.delete" {
		delete(values, request.Key)
	} else {
		values[request.Key] = request.Value
	}
	data, err := json.Marshal(values)
	if err != nil || len(values) > 64 || len(data) > maxStorageBytes {
		return fail("quota", "plugin storage exceeds 64 keys or 1 MiB")
	}
	if ctx.Err() != nil {
		return fail("canceled", "storage operation canceled")
	}
	temporary, err := os.CreateTemp(directory, ".values-*")
	if err != nil {
		return fail("storage_error", "cannot create storage transaction")
	}
	defer os.Remove(temporary.Name())
	_, writeErr := temporary.Write(data)
	syncErr := temporary.Sync()
	closeErr := temporary.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil || ctx.Err() != nil || root.Rename(filepath.Base(temporary.Name()), "values.json") != nil {
		return fail("storage_error", "cannot save plugin storage")
	}
	return Reply{Result: map[string]bool{"ok": true}}
}
