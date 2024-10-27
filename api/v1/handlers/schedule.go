// handlers/schedule.go
package handlers

import (
	"net/http"
	"strconv"

	"smuggr.xyz/optivum-bsf/common/models"
	"smuggr.xyz/optivum-bsf/core/datastore"
	"smuggr.xyz/optivum-bsf/core/scraper"

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
	Respond(c, http.StatusOK, scraper.DivisionsScraperResource.Designators.GetDesignators())
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
	Respond(c, http.StatusOK, scraper.TeachersScraperResource.Designators.GetDesignators())
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
	Respond(c, http.StatusOK, scraper.RoomsScraperResource.Designators.GetDesignators())
}
