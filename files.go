package rtapi

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

// FilePriority is how rTorrent treats one file of a torrent.
type FilePriority int64

const (
	FileSkip   FilePriority = 0 // not downloaded
	FileNormal FilePriority = 1
	FileHigh   FilePriority = 2
)

// File is one file of a torrent.
type File struct {
	Index           int    // the file's position in the torrent
	Path            string // relative to the torrent's data directory
	Size            uint64 // bytes
	Chunks          uint64
	CompletedChunks uint64
	Priority        FilePriority
}

// Complete reports whether every chunk of the file has been downloaded.
func (f File) Complete() bool {
	return f.CompletedChunks >= f.Chunks
}

// fileFields are the f.* getters Files reads, in order.
var fileFields = []string{"f.path", "f.size_bytes", "f.size_chunks", "f.completed_chunks", "f.priority"}

// Files lists a torrent's files.
func (r *Rtorrent) Files(hash string) ([]File, error) {
	return r.FilesContext(context.Background(), hash)
}

// FilesContext is Files with a context.
func (r *Rtorrent) FilesContext(ctx context.Context, hash string) ([]File, error) {
	if strings.TrimSpace(hash) == "" {
		return nil, fmt.Errorf("rtapi: torrent hash must not be empty")
	}
	params := []xmlrpcValue{newStringValue(hash), newStringValue("")}
	for _, field := range fileFields {
		params = append(params, newStringValue(field+"="))
	}
	value, err := r.call(ctx, "f.multicall", params...)
	if err != nil {
		return nil, fmt.Errorf("rtapi: list files: %w", err)
	}
	rows, err := value.arrayValues()
	if err != nil {
		return nil, fmt.Errorf("rtapi: list files: %w", err)
	}
	files := make([]File, 0, len(rows))
	for i, row := range rows {
		file, err := parseFile(i, row)
		if err != nil {
			return nil, fmt.Errorf("rtapi: parse file %d: %w", i, err)
		}
		files = append(files, file)
	}
	return files, nil
}

func parseFile(index int, row xmlrpcValue) (File, error) {
	fields, err := row.arrayValues()
	if err != nil {
		return File{}, err
	}
	if len(fields) < len(fileFields) {
		return File{}, fmt.Errorf("expected %d fields, got %d", len(fileFields), len(fields))
	}
	file := File{Index: index}
	if file.Path, err = fields[0].stringValue(); err != nil {
		return File{}, fmt.Errorf("path: %w", err)
	}
	if file.Size, err = fields[1].uint64Value(); err != nil {
		return File{}, fmt.Errorf("size: %w", err)
	}
	if file.Chunks, err = fields[2].uint64Value(); err != nil {
		return File{}, fmt.Errorf("chunks: %w", err)
	}
	if file.CompletedChunks, err = fields[3].uint64Value(); err != nil {
		return File{}, fmt.Errorf("completed chunks: %w", err)
	}
	priority, err := fields[4].int64Value()
	if err != nil {
		return File{}, fmt.Errorf("priority: %w", err)
	}
	file.Priority = FilePriority(priority)
	return file, nil
}

// SetFilePriorities sets the priorities of a torrent's files, keyed by file
// index, and has rTorrent apply them.
func (r *Rtorrent) SetFilePriorities(hash string, priorities map[int]FilePriority) error {
	return r.SetFilePrioritiesContext(context.Background(), hash, priorities)
}

// SetFilePrioritiesContext is SetFilePriorities with a context.
func (r *Rtorrent) SetFilePrioritiesContext(ctx context.Context, hash string, priorities map[int]FilePriority) error {
	if strings.TrimSpace(hash) == "" {
		return fmt.Errorf("rtapi: torrent hash must not be empty")
	}
	if len(priorities) == 0 {
		return nil
	}
	indexes := make([]int, 0, len(priorities))
	for index, priority := range priorities {
		if index < 0 {
			return fmt.Errorf("rtapi: file index %d is negative", index)
		}
		if priority < FileSkip || priority > FileHigh {
			return fmt.Errorf("rtapi: file priority %d is not FileSkip, FileNormal, or FileHigh", priority)
		}
		indexes = append(indexes, index)
	}
	slices.Sort(indexes)

	calls := make([]xmlrpcValue, 0, len(indexes)+1)
	for _, index := range indexes {
		target := fmt.Sprintf("%s:f%d", hash, index)
		calls = append(calls, newMethodCallValues("f.priority.set", newStringValue(target), newIntValue(int64(priorities[index]))))
	}
	calls = append(calls, newMethodCall("d.update_priorities", hash))
	req, err := marshalMethodCall(xmlrpcMethodCall{
		MethodName: "system.multicall",
		Params:     []xmlrpcParam{{Value: newArrayValue(calls...)}},
	})
	if err != nil {
		return err
	}
	if _, err := r.executeMulticall(ctx, req, len(calls)); err != nil {
		return fmt.Errorf("rtapi: set file priorities: %w", err)
	}
	return nil
}
