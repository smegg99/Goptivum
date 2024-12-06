// handlers/schedule.go
package handlers

import (
	"net/http"
	"strconv"

	"smuggr.xyz/goptivum/common/models"
	"smuggr.xyz/goptivum/core/datastore"

	"github.com/gin-gonic/gin"
)

func GetDivisionHandler(c *gin.Context) {
	index, err := strconv.ParseInt(c.Param("index"), 10, 64)
	if err != nil {
		Respond(c, http.StatusBadRequest, models.APIResponse{
			Message: "invalid index",
			Success: false,
		})

		return
	}

	division, err := datastore.GetDivision(index)
	if err != nil {
		if division == nil {
			Respond(c, http.StatusNotFound, models.APIResponse{
				Message: "division not found",
				Success: false,
			})
			return
		}

		Respond(c, http.StatusInternalServerError, models.APIResponse{
			Message: err.Error(),
			Success: false,
		})
		return
	}

	Respond(c, http.StatusOK, division)
}

func GetDivisionsHandler(c *gin.Context) {
	divisions, err := datastore.GetDivisionsMeta()
	if err != nil {
		Respond(c, http.StatusInternalServerError, models.APIResponse{
			Message: err.Error(),
			Success: false,
		})
		return
	}

	Respond(c, http.StatusOK, divisions)
}

func GetTeacherHandler(c *gin.Context) {
	index, err := strconv.ParseInt(c.Param("index"), 10, 64)
	if err != nil {
		Respond(c, http.StatusBadRequest, models.APIResponse{
			Message: "invalid index",
			Success: false,
		})
		return
	}

	teacher, err := datastore.GetTeacher(index)
	if err != nil {
		if teacher == nil {
			Respond(c, http.StatusNotFound, models.APIResponse{
				Message: "teacher not found",
				Success: false,
			})
			return
		}

		Respond(c, http.StatusInternalServerError, models.APIResponse{
			Message: err.Error(),
			Success: false,
		})
		return
	}

	Respond(c, http.StatusOK, teacher)
}

func GetTeachersHandler(c *gin.Context) {
	teachers, err := datastore.GetTeachersMeta()
	if err != nil {
		Respond(c, http.StatusInternalServerError, models.APIResponse{
			Message: err.Error(),
			Success: false,
		})
		return
	}

	Respond(c, http.StatusOK, teachers)
}

func GetRoomHandler(c *gin.Context) {
	index, err := strconv.ParseInt(c.Param("index"), 10, 64)
	if err != nil {
		Respond(c, http.StatusBadRequest, models.APIResponse{
			Message: "invalid index",
			Success: false,
		})
		return
	}

	room, err := datastore.GetRoom(index)
	if err != nil {
		if room == nil {
			Respond(c, http.StatusNotFound, models.APIResponse{
				Message: "room not found",
				Success: false,
			})
			return
		}

		Respond(c, http.StatusInternalServerError, models.APIResponse{
			Message: err.Error(),
			Success: false,
		})
		return
	}

	Respond(c, http.StatusOK, room)
}

func GetRoomsHandler(c *gin.Context) {
	rooms, err := datastore.GetRoomsMeta()
	if err != nil {
		Respond(c, http.StatusInternalServerError, models.APIResponse{
			Message: err.Error(),
			Success: false,
		})
		return
	}

	Respond(c, http.StatusOK, rooms)
}

func GetTeachersOnDutyWeekHandler(c *gin.Context) {
	teachers, err := datastore.GetTeachersOnDutyWeek()
	if err != nil {
		Respond(c, http.StatusInternalServerError, models.APIResponse{
			Message: err.Error(),
			Success: false,
		})
		return
	}

	Respond(c, http.StatusOK, teachers)
}

func GetPracticesHandler(c *gin.Context) {
	practices, err := datastore.GetPractices()
	if err != nil {
		Respond(c, http.StatusInternalServerError, models.APIResponse{
			Message: err.Error(),
			Success: false,
		})
		return
	}

	Respond(c, http.StatusOK, practices)
}