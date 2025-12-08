// core/parser/school/school.go
package school

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"smegg.me/goptivum/core/models"
	"smegg.me/goptivum/core/parser/classroom"
	"smegg.me/goptivum/core/parser/division"
	"smegg.me/goptivum/core/parser/teacher"
)

type Parser struct {
	School *models.School
}

func New() *Parser {
	return &Parser{
		School: &models.School{},
	}
}

func (sp *Parser) ParseDirectory(dir string) error {
	files, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("failed to read directory: %w", err)
	}

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		fileName := file.Name()
		filePath := filepath.Join(dir, fileName)

		f, err := os.Open(filePath)
		if err != nil {
			continue
		}

		if strings.HasPrefix(fileName, "n") && strings.HasSuffix(fileName, ".html") {
			tp := teacher.New()
			if err := tp.Parse(f); err == nil {
				sp.School.Teachers = append(sp.School.Teachers, *tp.Teacher)
			}
		} else if strings.HasPrefix(fileName, "o") && strings.HasSuffix(fileName, ".html") {
			dp := division.New()
			if err := dp.Parse(f); err == nil {
				sp.School.Divisions = append(sp.School.Divisions, *dp.Division)
			}
		} else if strings.HasPrefix(fileName, "s") && strings.HasSuffix(fileName, ".html") {
			cp := classroom.New()
			if err := cp.Parse(f); err == nil {
				sp.School.Classrooms = append(sp.School.Classrooms, *cp.Classroom)
			}
		}

		f.Close()
	}

	return nil
}
