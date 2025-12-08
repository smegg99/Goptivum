// core/models/schedule.go
package models

type DayOfWeek int

const (
	Monday DayOfWeek = iota
	Tuesday
	Wednesday
	Thursday
	Friday
	Saturday
	Sunday
)

func (d DayOfWeek) String() string {
	switch d {
	case Monday:
		return "Monday"
	case Tuesday:
		return "Tuesday"
	case Wednesday:
		return "Wednesday"
	case Thursday:
		return "Thursday"
	case Friday:
		return "Friday"
	case Saturday:
		return "Saturday"
	case Sunday:
		return "Sunday"
	default:
		return "Unknown"
	}
}

type Schedule struct {
	Monday    []Lesson
	Tuesday   []Lesson
	Wednesday []Lesson
	Thursday  []Lesson
	Friday    []Lesson
	Saturday  []Lesson
	Sunday    []Lesson
}

func (s *Schedule) ForEachDay(f func(day DayOfWeek, lessons []Lesson)) {
	f(Monday, s.Monday)
	f(Tuesday, s.Tuesday)
	f(Wednesday, s.Wednesday)
	f(Thursday, s.Thursday)
	f(Friday, s.Friday)
	f(Saturday, s.Saturday)
	f(Sunday, s.Sunday)
}

func (s *Schedule) ForEachWorkday(f func(day DayOfWeek, lessons []Lesson)) {
	f(Monday, s.Monday)
	f(Tuesday, s.Tuesday)
	f(Wednesday, s.Wednesday)
	f(Thursday, s.Thursday)
	f(Friday, s.Friday)
}
