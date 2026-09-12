// formats/format.go

// Package formats turns a file a school handed us into an arrango SchoolModel.
//
// Four properties shape everything here:
//
//   - Findings, not failure. An import that maps most of a school returns the
//     school plus typed Findings for what it dropped. A file that fails
//     wholesale teaches a secretary nothing.
//   - Detection is by content. A ".zip" could be anything.
//   - Importers never assign arrango_id. They emit local ids plus source_ref
//     and the server maps those to durable ids on insert, which is what makes
//     a re-import an update instead of a duplicate.
//   - Export is a separate optional interface. Not every format round-trips.
package formats

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"io/fs"
	"path"
	"sort"
	"sync"
	"time"

	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"
)

// Source is one thing a user handed us.
type Source struct {
	Name string // original filename, for messages only
	FS   fs.FS  // zip or directory, normalised
}

// Confidence is how sure an importer is that a Source is its own format.
type Confidence uint8

const (
	No Confidence = iota
	Maybe
	Certain
)

func (c Confidence) String() string {
	switch c {
	case Certain:
		return "certain"
	case Maybe:
		return "maybe"
	default:
		return "no"
	}
}

// Importer reads one source format.
type Importer interface {
	ID() string // stable; namespaces source_ref
	Detect(Source) (Confidence, error)
	Import(context.Context, Source) (*Result, error)
}

// Exporter writes one target format. Not every Importer has one.
type Exporter interface {
	ID() string
	Export(context.Context, *Result) ([]byte, error)
}

// Result is one import: the school, the timetable it carried, and what did
// not map.
type Result struct {
	Model    *arrangov1.SchoolModel
	Schedule *arrangov1.ScheduleSnapshot // the published timetable, if carried
	Findings []Finding
}

// HasErrors reports whether anything failed to map badly enough to lose data.
func (r *Result) HasErrors() bool {
	for _, f := range r.Findings {
		if f.Severity == Error {
			return true
		}
	}
	return false
}

type Severity uint8

const (
	Info Severity = iota
	Warning
	Error
)

func (s Severity) String() string {
	switch s {
	case Error:
		return "error"
	case Warning:
		return "warning"
	default:
		return "info"
	}
}

// FindingKind is the stable, localisable identity of a finding. The UI renders
// from this and the entity refs, never from Detail.
type FindingKind string

const (
	// An element the reader does not consume, with its count in Detail.
	KindUnmappedElement FindingKind = "unmapped_element"
	// An attribute carrying data the SchoolModel has no place for.
	KindUnmappedAttribute FindingKind = "unmapped_attribute"
	// A code reference that resolves to nothing, degrading one entity.
	KindUnknownReference FindingKind = "unknown_reference"
	// A lesson the source describes but the reader could not build.
	KindLessonDropped FindingKind = "lesson_dropped"
	// The source carries no placement for a lesson.
	KindLessonUnplaced FindingKind = "lesson_unplaced"
	// Split OPEN/FIXED was inferred, because the source does not state it.
	KindSplitKindInferred FindingKind = "split_kind_inferred"
	// A group reference disagrees with the group it resolves to.
	KindGroupOrdinalMismatch FindingKind = "group_ordinal_mismatch"
	// A multi-period lesson changes room part way through.
	KindBlockRoomChange FindingKind = "block_room_change"
	// A block's continuation rows are missing, short or circular.
	KindBrokenBlockChain FindingKind = "broken_block_chain"
	// The source allows any room, so no room eligibility was set.
	KindRoomPreferenceOpen FindingKind = "room_preference_open"
	// A timetable slot the source marks but the model cannot express.
	KindUnmappedSlotMark FindingKind = "unmapped_slot_mark"
	// The file defines no days or no periods, so nothing can be scheduled.
	KindEmptyCalendar FindingKind = "empty_calendar"
	// A wrapped parser's own warning; Detail carries its prose.
	KindSourceWarning FindingKind = "source_warning"
)

// Finding is what did not map. Typed and located; the UI localises from Kind
// and the refs, never from Detail.
type Finding struct {
	Kind     FindingKind
	Severity Severity
	Entities []*arrangov1.EntityRef
	Detail   string // debug hint only, never rendered
}

// Ref is a typed entity reference for a Finding.
func Ref(kind arrangov1.EntityKind, id uint32) *arrangov1.EntityRef {
	return &arrangov1.EntityRef{Kind: kind, Id: id}
}

// Registry maps importer ID to Importer.
type Registry struct {
	mu    sync.RWMutex
	byID  map[string]Importer
	order []string
}

// NewRegistry returns a registry holding the given importers.
func NewRegistry(importers ...Importer) *Registry {
	r := &Registry{byID: map[string]Importer{}}
	for _, imp := range importers {
		r.Register(imp)
	}
	return r
}

// Register adds an importer. It panics on an empty or duplicate ID, because
// both are programming mistakes that would silently shadow a format.
func (r *Registry) Register(imp Importer) {
	if imp == nil {
		panic("formats: Register(nil)")
	}
	id := imp.ID()
	if id == "" {
		panic("formats: importer with an empty ID")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.byID == nil {
		r.byID = map[string]Importer{}
	}
	if _, exists := r.byID[id]; exists {
		panic("formats: duplicate importer ID " + id)
	}
	r.byID[id] = imp
	r.order = append(r.order, id)
}

// Importer returns the importer with this ID.
func (r *Registry) Importer(id string) (Importer, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	imp, ok := r.byID[id]
	return imp, ok
}

// Importers returns every importer in registration order.
func (r *Registry) Importers() []Importer {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Importer, 0, len(r.order))
	for _, id := range r.order {
		out = append(out, r.byID[id])
	}
	return out
}

// Open normalises one uploaded file into a Source: a zip becomes its own
// filesystem, anything else a filesystem holding that single file.
func Open(name string, data []byte) Source {
	if reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data))); err == nil {
		return Source{Name: name, FS: reader}
	}
	return Source{Name: name, FS: singleFileFS{name: entryName(name), data: data}}
}

// entryName reduces an upload name to one valid fs.FS path element.
func entryName(name string) string {
	base := path.Base(name)
	if base == "" || base == "." || base == "/" || !fs.ValidPath(base) {
		return "upload"
	}
	return base
}

// singleFileFS is one file presented as a filesystem, so that detection can
// walk every Source the same way.
type singleFileFS struct {
	name string
	data []byte
}

func (s singleFileFS) Open(name string) (fs.File, error) {
	if name != s.name {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return &singleFile{fs: s, Reader: bytes.NewReader(s.data)}, nil
}

func (s singleFileFS) ReadFile(name string) ([]byte, error) {
	if name != s.name {
		return nil, &fs.PathError{Op: "read", Path: name, Err: fs.ErrNotExist}
	}
	out := make([]byte, len(s.data))
	copy(out, s.data)
	return out, nil
}

func (s singleFileFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if name != "." {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrNotExist}
	}
	return []fs.DirEntry{fs.FileInfoToDirEntry(fileInfo{name: s.name, size: int64(len(s.data))})}, nil
}

func (s singleFileFS) Stat(name string) (fs.FileInfo, error) {
	switch name {
	case ".":
		return fileInfo{name: ".", dir: true}, nil
	case s.name:
		return fileInfo{name: s.name, size: int64(len(s.data))}, nil
	}
	return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrNotExist}
}

type singleFile struct {
	fs singleFileFS
	*bytes.Reader
}

func (f *singleFile) Stat() (fs.FileInfo, error) {
	return fileInfo{name: f.fs.name, size: int64(len(f.fs.data))}, nil
}

func (f *singleFile) Close() error { return nil }

type fileInfo struct {
	name string
	size int64
	dir  bool
}

func (i fileInfo) Name() string { return i.name }
func (i fileInfo) Size() int64  { return i.size }
func (i fileInfo) Mode() fs.FileMode {
	if i.dir {
		return fs.ModeDir | 0o555
	}
	return 0o444
}
func (i fileInfo) ModTime() time.Time { return time.Time{} }
func (i fileInfo) IsDir() bool        { return i.dir }
func (i fileInfo) Sys() any           { return nil }

// Files returns every regular file in a Source, in stable path order.
func Files(src Source) ([]string, error) {
	if src.FS == nil {
		return nil, nil
	}
	var out []string
	err := fs.WalkDir(src.FS, ".", func(p string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			out = append(out, p)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

// Head reads at most n bytes from a file, for content-based detection.
func Head(fsys fs.FS, name string, n int) ([]byte, error) {
	file, err := fsys.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	head := make([]byte, n)
	read, err := io.ReadFull(file, head)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return nil, err
	}
	return head[:read], nil
}
