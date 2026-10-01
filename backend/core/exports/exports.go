// core/exports/exports.go

// Package exports builds a version or a result as a file in another format
// once, keeps it in object storage and answers every later request for the
// same file from there.
package exports

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/smegg99/s99ab/data/storage"
	"golang.org/x/sync/singleflight"

	"github.com/smegg99/goptivum/backend/core/documents"
	"github.com/smegg99/goptivum/backend/core/plans"
	"github.com/smegg99/goptivum/backend/formats"
)

// ErrUnknownFormat means no exporter answers to the format asked for.
var ErrUnknownFormat = errors.New("exports: no such format")

// Source is what a file is built from: one version or one result of a plan,
// and the stored document it names.
type Source struct {
	PlanID int64
	// Version is set for a version, ResultID for a result; the other is zero.
	Version   int64
	ResultID  int64
	ObjectKey string
}

// Key is where the file built from source in format is kept.
func Key(source Source, format string) string {
	if source.ResultID != 0 {
		return fmt.Sprintf("exports/%d/results/%d/%s", source.PlanID, source.ResultID, format)
	}
	return fmt.Sprintf("exports/%d/versions/%d/%s", source.PlanID, source.Version, format)
}

// Format is one kind of file the service builds.
type Format struct {
	Exporter    formats.Exporter
	ContentType string
}

// Variant is an exporter whose bytes depend on more than the document, such
// as the key a file is encrypted to; a kept file built with another variant is
// built again.
type Variant interface {
	Variant(ctx context.Context) (string, error)
}

// File is a built file and its media type.
type File struct {
	Bytes       []byte
	ContentType string
}

// Service builds and keeps exported files.
type Service struct {
	index     *plans.Repository
	documents *documents.Store
	objects   storage.Storage
	formats   map[string]Format
	// flight makes the requests for one file that arrive while it is being
	// built wait for that build.
	flight singleflight.Group
}

// New builds a service over the plan index, the stored documents and the
// object storage the files are kept in, for the given formats.
func New(index *plans.Repository, docs *documents.Store, objects storage.Storage, list ...Format) (*Service, error) {
	if index == nil || docs == nil || objects == nil {
		return nil, errors.New("exports: the plan index, the documents and the object storage are all required")
	}
	byID := make(map[string]Format, len(list))
	for _, format := range list {
		if format.Exporter == nil || format.ContentType == "" {
			return nil, errors.New("exports: a format needs an exporter and a content type")
		}
		id := format.Exporter.ID()
		if _, taken := byID[id]; taken {
			return nil, fmt.Errorf("exports: format %s given twice", id)
		}
		byID[id] = format
	}
	return &Service{index: index, documents: docs, objects: objects, formats: byID}, nil
}

// File answers the file built from source in format: the kept one when it is
// current, otherwise one built now and kept. A source that is gone is
// plans.ErrNoVersion or plans.ErrNoResult; an exporter's refusal, such as
// formats.ErrNoTimetable, is returned as it is.
func (s *Service) File(ctx context.Context, source Source, format string) (File, error) {
	chosen, ok := s.formats[format]
	if !ok {
		return File{}, fmt.Errorf("%w: %q", ErrUnknownFormat, format)
	}
	key := Key(source, format)
	variant, err := variantOf(ctx, chosen.Exporter)
	if err != nil {
		return File{}, fmt.Errorf("exports: %s: %w", key, err)
	}
	if data, ok, err := s.kept(ctx, key, variant); err != nil || ok {
		return File{Bytes: data, ContentType: chosen.ContentType}, err
	}

	// Detached, so one caller that leaves does not fail the build the others
	// wait for.
	build := context.WithoutCancel(ctx)
	answer := s.flight.DoChan(key, func() (any, error) {
		return s.build(build, source, key, format, chosen, variant)
	})
	select {
	case <-ctx.Done():
		return File{}, ctx.Err()
	case done := <-answer:
		if done.Err != nil {
			return File{}, done.Err
		}
		return File{Bytes: done.Val.([]byte), ContentType: chosen.ContentType}, nil
	}
}

func variantOf(ctx context.Context, exporter formats.Exporter) (string, error) {
	if varied, ok := exporter.(Variant); ok {
		return varied.Variant(ctx)
	}
	return "", nil
}

// kept reads the file kept under key when it was built with variant and its
// object is whole; false means it has to be built.
func (s *Service) kept(ctx context.Context, key, variant string) ([]byte, bool, error) {
	row, err := s.index.Export(ctx, key)
	if errors.Is(err, plans.ErrNoExport) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if row.Variant != variant {
		return nil, false, nil
	}
	reader, err := s.objects.Get(ctx, key)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("exports: get %s: %w", key, err)
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, row.SizeBytes+1))
	if err != nil {
		return nil, false, fmt.Errorf("exports: read %s: %w", key, err)
	}
	if int64(len(data)) != row.SizeBytes {
		return nil, false, nil
	}
	return data, true, nil
}

// build makes the file, stores it and records it.
func (s *Service) build(ctx context.Context, source Source, key, format string, chosen Format, variant string) ([]byte, error) {
	// A build that ended just before this one began has kept the file.
	if data, ok, err := s.kept(ctx, key, variant); err != nil || ok {
		return data, err
	}
	doc, err := s.documents.Load(ctx, source.ObjectKey)
	if err != nil {
		return nil, fmt.Errorf("exports: %s: %w", key, err)
	}
	data, err := chosen.Exporter.Export(ctx, &formats.Result{
		Model: doc.GetModel(), Schedule: doc.GetSchedule(), Document: doc,
	})
	if err != nil {
		return nil, err
	}

	// Revived before the write, so a sweep that finds the key unnamed cannot
	// delete the bytes between this write and the row that names them.
	if err := s.index.Revive(ctx, key); err != nil {
		return nil, err
	}
	if err := s.objects.Put(ctx, key, bytes.NewReader(data), chosen.ContentType); err != nil {
		return nil, fmt.Errorf("exports: put %s: %w", key, err)
	}
	if _, err := s.index.KeepExport(ctx, plans.Export{
		ObjectKey: key, PlanID: source.PlanID, Version: source.Version, ResultID: source.ResultID,
		Format: format, Variant: variant, SizeBytes: int64(len(data)),
	}); err != nil {
		// Best effort: the stored bytes wait for the sweep instead of staying.
		_ = s.index.Discard(ctx, key)
		return nil, err
	}
	return data, nil
}
