// api/v1/library.go

package v1

import (
	"context"
	"errors"
	"strconv"

	"github.com/smegg99/goptivum/backend/api/v1/gen"
	"github.com/smegg99/goptivum/backend/common/messages"
	"github.com/smegg99/goptivum/backend/core/library"
)

// Libraries serves each account's own Library arrangement.
type Libraries struct {
	libraries *library.Repository
}

// NewLibraries wires the Library routes; with a nil repository they answer that the store is unavailable.
func NewLibraries(repository *library.Repository) *Libraries {
	return &Libraries{libraries: repository}
}

// GetAccountLibrary is the caller's arrangement, version 0 when it never saved one.
func (l *Libraries) GetAccountLibrary(ctx context.Context, _ gen.GetAccountLibraryRequestObject) (gen.GetAccountLibraryResponseObject, error) {
	caller, ok := callerID(ctx)
	if !ok {
		return gen.GetAccountLibrary401JSONResponse{UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(unauthorized())}, nil
	}
	if l == nil || l.libraries == nil {
		return gen.GetAccountLibrary503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}
	read, err := l.libraries.Get(ctx, caller)
	if err != nil {
		return gen.GetAccountLibrary503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}
	return gen.GetAccountLibrary200JSONResponse(libraryBody(read)), nil
}

// SaveAccountLibrary replaces the caller's arrangement when the version it sends is the stored one.
func (l *Libraries) SaveAccountLibrary(ctx context.Context, request gen.SaveAccountLibraryRequestObject) (gen.SaveAccountLibraryResponseObject, error) {
	caller, ok := callerID(ctx)
	if !ok {
		return gen.SaveAccountLibrary401JSONResponse{UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(unauthorized())}, nil
	}
	if l == nil || l.libraries == nil {
		return gen.SaveAccountLibrary503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}
	if request.Body == nil {
		return gen.SaveAccountLibrary422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(validation("body", "required"))}, nil
	}
	next, err := libraryOf(*request.Body)
	var invalid *library.Invalid
	if errors.As(err, &invalid) {
		return gen.SaveAccountLibrary422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(validation(invalid.Field, invalid.Code))}, nil
	}
	saved, err := l.libraries.Save(ctx, caller, next)
	var stale *library.Stale
	switch {
	case errors.As(err, &stale):
		return gen.SaveAccountLibrary409JSONResponse{
			Code: gen.AccountLibraryConflictCodeConflict, Message: messages.KeyAccountLibraryConflict,
			Current: libraryBody(stale.Current),
		}, nil
	case errors.As(err, &invalid):
		return gen.SaveAccountLibrary422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(validation(invalid.Field, invalid.Code))}, nil
	case err != nil:
		return gen.SaveAccountLibrary503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}
	return gen.SaveAccountLibrary200JSONResponse(libraryBody(saved)), nil
}

// libraryOf maps the wire arrangement to the repository's; a project id that is not one is an *library.Invalid.
func libraryOf(body gen.AccountLibrary) (library.Library, error) {
	var err error
	ids := func(field string, raw []string) []int64 {
		out := make([]int64, 0, len(raw))
		for _, one := range raw {
			id, parseErr := strconv.ParseInt(one, 10, 64)
			if parseErr != nil || id <= 0 {
				if err == nil {
					err = &library.Invalid{Field: field, Code: "invalid"}
				}
				continue
			}
			out = append(out, id)
		}
		return out
	}
	next := library.Library{Version: body.Version, Folders: make([]library.Folder, 0, len(body.Folders))}
	for _, folder := range body.Folders {
		one := library.Folder{ID: folder.Id, Name: folder.Name, Accent: folder.Accent, Pinned: folder.Pinned,
			Projects: ids("folders.projects", folder.Projects)}
		if folder.ParentId != nil {
			one.Parent = *folder.ParentId
		}
		next.Folders = append(next.Folders, one)
	}
	next.Projects = ids("projects", body.Projects)
	next.Pinned = ids("pinned", body.Pinned)
	next.Recent = ids("recent", body.Recent)
	return next, err
}

func libraryBody(arrangement library.Library) gen.AccountLibrary {
	ids := func(raw []int64) []string {
		out := make([]string, 0, len(raw))
		for _, id := range raw {
			out = append(out, strconv.FormatInt(id, 10))
		}
		return out
	}
	body := gen.AccountLibrary{
		Version: arrangement.Version, Folders: make([]gen.LibraryFolder, 0, len(arrangement.Folders)),
		Projects: ids(arrangement.Projects), Pinned: ids(arrangement.Pinned), Recent: ids(arrangement.Recent),
	}
	for _, folder := range arrangement.Folders {
		one := gen.LibraryFolder{Id: folder.ID, Name: folder.Name, Accent: folder.Accent, Pinned: folder.Pinned,
			Projects: ids(folder.Projects)}
		if folder.Parent != "" {
			parent := folder.Parent
			one.ParentId = &parent
		}
		body.Folders = append(body.Folders, one)
	}
	return body
}
