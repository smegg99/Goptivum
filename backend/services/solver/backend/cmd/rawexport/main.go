// Command rawexport parses an Optivum HTML-export zip and writes ONLY the raw
// school model (divisions, years, groups, teachers, rooms, subjects, lessons and
// their constraints) as JSON — with the imported timetable stripped out, so the
// result is a clean problem to solve from scratch, not a pre-solved schedule.
//
// Usage: go run ./cmd/rawexport <input.zip> <output.json>
package main

import (
	"fmt"
	"os"

	"google.golang.org/protobuf/encoding/protojson"

	"github.com/smegg99/arrango/backend/optivum"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: rawexport <input.zip> <output.json>")
		os.Exit(2)
	}
	inPath, outPath := os.Args[1], os.Args[2]

	data, err := os.ReadFile(inPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read %s: %v\n", inPath, err)
		os.Exit(1)
	}
	result, err := optivum.ParseZip(data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse: %v\n", err)
		os.Exit(1)
	}

	// Strip every trace of the imported schedule from the lessons: no previous
	// placement, no locks. Everything else (participants, subject, teacher,
	// duration, room designators) is the raw problem and is kept.
	model := result.Model
	for _, lesson := range model.GetLessons() {
		lesson.HasPrevious = false
		lesson.PreviousPlacement = nil
		lesson.Locked = false
		lesson.LockedPlacement = nil
	}

	out, err := protojson.MarshalOptions{EmitUnpopulated: true, Multiline: true, Indent: "  "}.Marshal(model)
	if err != nil {
		fmt.Fprintf(os.Stderr, "marshal: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(outPath, out, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write %s: %v\n", outPath, err)
		os.Exit(1)
	}

	// Report what came out, and confirm no schedule leaked through.
	withPlacement := 0
	for _, l := range model.GetLessons() {
		if l.GetHasPrevious() || l.GetLockedPlacement() != nil {
			withPlacement++
		}
	}
	fmt.Printf("wrote %s: divisions=%d groups=%d teachers=%d rooms=%d subjects=%d lessons=%d (lessons with a schedule=%d)\n",
		outPath, len(model.GetDivisions()), len(model.GetGroups()),
		len(model.GetTeachers()), len(model.GetRooms()), len(model.GetSubjects()),
		len(model.GetLessons()), withPlacement)
	if n := len(result.Warnings); n > 0 {
		fmt.Printf("import warnings: %d\n", n)
	}
}
