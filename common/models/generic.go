// models/generic.go
package models

type OtherChannels struct {
	Clients chan int64;
}

type ScheduleChannels struct {
	Divisons chan int64;
	Teachers chan int64;
	Rooms    chan int64;
}