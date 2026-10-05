// api/v1/exports.go

package v1

import (
	"bytes"
	"context"
	"errors"
	"strconv"

	"github.com/smegg99/s99logger"

	"github.com/smegg99/goptivum/backend/api/v1/gen"
	"github.com/smegg99/goptivum/backend/common/logger"
	"github.com/smegg99/goptivum/backend/common/messages"
	"github.com/smegg99/goptivum/backend/core/exports"
	"github.com/smegg99/goptivum/backend/formats"
)

// Exports serves a version or a result as a file in another format, over the
// plan handlers and the export service.
type Exports struct {
	api     *API
	service *exports.Service
}

// NewExports wires the export routes; with a nil service they answer 404.
func NewExports(api *API, service *exports.Service) *Exports {
	return &Exports{api: api, service: service}
}

// exportAnswer is what an export route answers, before it is put in the
// operation's own response types.
type exportAnswer struct {
	status int
	file   exports.File
	body   gen.Error
}

const (
	exportOK = iota
	exportBadRequest
	exportUnauthorized
	exportNotFound
	exportRefused
	exportUnavailable
)

// ExportVersion is one version of any live plan as a file.
func (x *Exports) ExportVersion(ctx context.Context, request gen.ExportVersionRequestObject) (gen.ExportVersionResponseObject, error) {
	answer, err := x.export(ctx, request.Format, func() (exports.Source, error) {
		plan, err := x.api.readable(ctx, request.PlanId)
		if err != nil {
			return exports.Source{}, err
		}
		version, err := x.api.plans.Version(ctx, plan.ID, request.Version)
		if err != nil {
			return exports.Source{}, err
		}
		return exports.Source{PlanID: plan.ID, Version: version.Version, ObjectKey: version.ObjectKey}, nil
	})
	if err != nil {
		return nil, err
	}
	switch answer.status {
	case exportOK:
		if request.Format == gen.OptivumHtml {
			return gen.ExportVersion200ApplicationzipResponse{Body: bytes.NewReader(answer.file.Bytes), ContentLength: int64(len(answer.file.Bytes))}, nil
		}
		return gen.ExportVersion200ApplicationoctetStreamResponse{Body: bytes.NewReader(answer.file.Bytes), ContentLength: int64(len(answer.file.Bytes))}, nil
	case exportBadRequest:
		return gen.ExportVersion400JSONResponse{BadRequestJSONResponse: gen.BadRequestJSONResponse(answer.body)}, nil
	case exportUnauthorized:
		return gen.ExportVersion401JSONResponse{UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(answer.body)}, nil
	case exportNotFound:
		return gen.ExportVersion404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(answer.body)}, nil
	case exportRefused:
		return gen.ExportVersion422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(answer.body)}, nil
	}
	return gen.ExportVersion503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(answer.body)}, nil
}

// ExportResult is one result of any live plan, out of Trash, as a file.
func (x *Exports) ExportResult(ctx context.Context, request gen.ExportResultRequestObject) (gen.ExportResultResponseObject, error) {
	answer, err := x.export(ctx, request.Format, func() (exports.Source, error) {
		plan, result, err := x.api.readableResult(ctx, request.PlanId, request.ResultId)
		if err != nil {
			return exports.Source{}, err
		}
		return exports.Source{PlanID: plan.ID, ResultID: result.ID, ObjectKey: result.ObjectKey}, nil
	})
	if err != nil {
		return nil, err
	}
	switch answer.status {
	case exportOK:
		if request.Format == gen.OptivumHtml {
			return gen.ExportResult200ApplicationzipResponse{Body: bytes.NewReader(answer.file.Bytes), ContentLength: int64(len(answer.file.Bytes))}, nil
		}
		return gen.ExportResult200ApplicationoctetStreamResponse{Body: bytes.NewReader(answer.file.Bytes), ContentLength: int64(len(answer.file.Bytes))}, nil
	case exportBadRequest:
		return gen.ExportResult400JSONResponse{BadRequestJSONResponse: gen.BadRequestJSONResponse(answer.body)}, nil
	case exportUnauthorized:
		return gen.ExportResult401JSONResponse{UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(answer.body)}, nil
	case exportNotFound:
		return gen.ExportResult404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(answer.body)}, nil
	case exportRefused:
		return gen.ExportResult422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(answer.body)}, nil
	}
	return gen.ExportResult503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(answer.body)}, nil
}

// export resolves the source and answers its file; source reads the plan and
// the version or result the path names, as every other read of them does.
func (x *Exports) export(ctx context.Context, format gen.ExportFormat, source func() (exports.Source, error)) (exportAnswer, error) {
	if x == nil || x.api == nil || x.service == nil {
		return exportAnswer{status: exportNotFound, body: notFound()}, nil
	}
	if _, ok := callerID(ctx); !ok {
		return exportAnswer{status: exportUnauthorized, body: unauthorized()}, nil
	}
	if !format.Valid() {
		return exportAnswer{status: exportBadRequest, body: gen.Error{Code: "bad_request", Message: messages.KeyErrorBadRequest}}, nil
	}
	from, err := source()
	if err == nil {
		var file exports.File
		file, err = x.service.File(ctx, from, string(format))
		if err == nil {
			return exportAnswer{status: exportOK, file: file}, nil
		}
	}
	switch {
	case missing(err):
		return exportAnswer{status: exportNotFound, body: notFound()}, nil
	case errors.Is(err, formats.ErrNoTimetable):
		return exportAnswer{status: exportRefused, body: gen.Error{Code: "validation", Message: messages.KeyExportNoTimetable}}, nil
	case errors.Is(err, context.Canceled):
		return exportAnswer{}, err
	}
	// The store, the database or the school key is out of reach: the file
	// may build on the next request.
	logger.Log.Warn(s99logger.NewEvent(logger.EventExportFailed,
		s99logger.String("plan", strconv.FormatInt(from.PlanID, 10)),
		s99logger.String("source", exportedFrom(from)),
		s99logger.String("format", string(format)), s99logger.Err(err)))
	return exportAnswer{status: exportUnavailable, body: unavailable()}, nil
}

// exportedFrom names a source for a log line.
func exportedFrom(source exports.Source) string {
	if source.ResultID != 0 {
		return "result " + strconv.FormatInt(source.ResultID, 10)
	}
	return "version " + strconv.FormatInt(source.Version, 10)
}
