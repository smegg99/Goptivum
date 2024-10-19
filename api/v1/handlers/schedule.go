// handlers/schedule.go
package handlers

import (
	"fmt"
	"net/http"
	"strconv"

	"smuggr.xyz/optivum-bsf/core/datastore"
	"smuggr.xyz/optivum-bsf/core/scraper"

	"github.com/gin-gonic/gin"
)

func GetDivisionHandler(c *gin.Context) {
	index, err := strconv.ParseInt(c.Param("index"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid index",
		})
		return
	}

	division, err := datastore.GetDivision(index)
	if err != nil {
		if division == nil {
			c.JSON(http.StatusNotFound, gin.H{
				"error": "division not found",
			})
			return
		}

		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	Respond(c, division)
}

func GetDivisionsHandler(c *gin.Context) {
	fmt.Println(scraper.DivisionsDesignators)
	Respond(c, scraper.DivisionsDesignators)
}

func GetTeacherHandler(c *gin.Context) {
	index, err := strconv.ParseInt(c.Param("index"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid index",
		})
		return
	}

	teacher, err := datastore.GetTeacher(index)
	if err != nil {
		if teacher == nil {
			c.JSON(http.StatusNotFound, gin.H{
				"error": "teacher not found",
			})
			return
		}

		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	Respond(c, teacher)
}

func GetTeachersHandler(c *gin.Context) {
	Respond(c, scraper.TeachersDesignators)
}

func GetRoomHandler(c *gin.Context) {
	index, err := strconv.ParseInt(c.Param("index"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid index",
		})
		return
	}

	room, err := datastore.GetRoom(index)
	if err != nil {
		if room == nil {
			c.JSON(http.StatusNotFound, gin.H{
				"error": "room not found",
			})
			return
		}

		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	Respond(c, room)
}

func GetRoomsHandler(c *gin.Context) {
	Respond(c, scraper.RoomsDesignators)
}
