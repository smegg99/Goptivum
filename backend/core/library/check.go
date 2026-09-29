// core/library/check.go

package library

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

var (
	folderIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	accentPattern   = regexp.MustCompile(`^[a-z0-9-]{0,32}$`)
)

// check refuses what an arrangement cannot hold as an *Invalid and trims the folder names.
func check(arrangement *Library) error {
	if arrangement.Version < 0 {
		return &Invalid{Field: "version", Code: "invalid"}
	}
	if err := checkFolders(arrangement.Folders); err != nil {
		return err
	}

	// A project sits in one place at most: one folder, or the top level's list.
	placed := map[int64]bool{}
	place := func(field string, ids []int64) error {
		for _, id := range ids {
			switch {
			case id <= 0:
				return &Invalid{Field: field, Code: "invalid"}
			case placed[id]:
				return &Invalid{Field: "projects", Code: "duplicate"}
			}
			placed[id] = true
		}
		return nil
	}
	for _, folder := range arrangement.Folders {
		if err := place("folders.projects", folder.Projects); err != nil {
			return err
		}
	}
	if err := place("projects", arrangement.Projects); err != nil {
		return err
	}
	if err := distinct("pinned", arrangement.Pinned); err != nil {
		return err
	}
	if len(arrangement.Recent) > MaxRecent {
		return &Invalid{Field: "recent", Code: "too_many"}
	}
	if err := distinct("recent", arrangement.Recent); err != nil {
		return err
	}
	if len(names(*arrangement)) > MaxProjects {
		return &Invalid{Field: "projects", Code: "too_many"}
	}
	return nil
}

// checkFolders refuses a malformed folder, one id twice, and a parent that is missing, circular or too deep.
func checkFolders(folders []Folder) error {
	if len(folders) > MaxFolders {
		return &Invalid{Field: "folders", Code: "too_many"}
	}
	parents := make(map[string]string, len(folders))
	for i := range folders {
		folder := &folders[i]
		switch {
		case !folderIDPattern.MatchString(folder.ID):
			return &Invalid{Field: "folders.id", Code: "invalid"}
		case hasKey(parents, folder.ID):
			return &Invalid{Field: "folders.id", Code: "duplicate"}
		}
		folder.Name = strings.TrimSpace(folder.Name)
		switch {
		case folder.Name == "":
			return &Invalid{Field: "folders.name", Code: "required"}
		case utf8.RuneCountInString(folder.Name) > MaxNameLength:
			return &Invalid{Field: "folders.name", Code: "too_long"}
		case !accentPattern.MatchString(folder.Accent):
			return &Invalid{Field: "folders.accent", Code: "invalid"}
		}
		parents[folder.ID] = folder.Parent
	}
	for _, folder := range folders {
		depth := 1
		for parent := folder.Parent; parent != ""; parent = parents[parent] {
			switch {
			case !hasKey(parents, parent):
				return &Invalid{Field: "folders.parent_id", Code: "unknown"}
			case depth > len(folders):
				return &Invalid{Field: "folders.parent_id", Code: "cycle"}
			}
			depth++
		}
		if depth > MaxDepth {
			return &Invalid{Field: "folders.parent_id", Code: "too_deep"}
		}
	}
	return nil
}

// distinct refuses an id below 1 or one id twice in a list.
func distinct(field string, ids []int64) error {
	seen := make(map[int64]bool, len(ids))
	for _, id := range ids {
		switch {
		case id <= 0:
			return &Invalid{Field: field, Code: "invalid"}
		case seen[id]:
			return &Invalid{Field: field, Code: "duplicate"}
		}
		seen[id] = true
	}
	return nil
}

func hasKey(parents map[string]string, id string) bool {
	_, ok := parents[id]
	return ok
}
