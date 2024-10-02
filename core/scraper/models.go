// scraper/models.go
package scraper

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type TimeString string

func (t TimeString) ToTimestamp() (Timestamp, error) {
	parts := strings.Split(string(t), ":")
	if len(parts) < 2 || len(parts) > 3 {
		return Timestamp{}, fmt.Errorf("invalid time format: %s", t)
	}

	hour, err := strconv.Atoi(parts[0])
	if err != nil {
		return Timestamp{}, fmt.Errorf("invalid hour: %w", err)
	}

	minute, err := strconv.Atoi(parts[1])
	if err != nil {
		return Timestamp{}, fmt.Errorf("invalid minute: %w", err)
	}

	return Timestamp{Hour: hour, Minute: minute}, nil
}

type Timestamp struct {
	Hour   int `json:"h"`
	Minute int `json:"m"`
}

func (t Timestamp) String() string {
	return fmt.Sprintf("%02d:%02d", t.Hour, t.Minute)
}

func (t Timestamp) Compare(time time.Time) bool {
	return t.Hour == time.Hour() && t.Minute == time.Minute()
}

type TimeRange struct {
	Start Timestamp `json:"start"`
	End   Timestamp `json:"end"`
}

func (tr TimeRange) String() string {
	return fmt.Sprintf("%s-%s", tr.Start, tr.End)
}

type Teacher struct {
	Designator string   `json:"designator"`
	FullName   string   `json:"full_name"`
	Schedule   Schedule `json:"schedule"`
}

type Room struct {
	Designator   string   `json:"designator"`
	FullName     string   `json:"full_name"`
	Schedule     Schedule `json:"schedule"`
	BuildingName string   `json:"building_name"`
}

type Lesson struct {
	FullName  string    `json:"full_name"`
	Teacher   string    `json:"teacher"`
	Room      string    `json:"room"`
	Division  string    `json:"division"`
	TimeRange TimeRange `json:"time_range"`
}

func (l Lesson) String() string {
	return fmt.Sprintf("[%s %s %s %s %s]", l.FullName, l.Teacher, l.Room, l.Division, l.TimeRange)
}

// Schedule is a 3D array of lessons, first dimension is day,
// second is the lesson number, third are the lessons
type Schedule [][][]Lesson

type Division struct {
	Index      uint     `json:"index"`
	Designator string   `json:"designator"`
	FullName   string   `json:"full_name"`
	Schedule   Schedule `json:"schedule"`
}