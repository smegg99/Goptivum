// handlers/schedule.go
package handlers

import (
	"net/http"
	"strconv"

	"smuggr.xyz/optivum-bsf/core/datastore"

	"github.com/gin-gonic/gin"
)

func GetDivisionHandler(c *gin.Context) {
	index, err := strconv.Atoi(c.Param("index"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid index",
		})
		return
	}

	division, err := datastore.GetDivision(uint32(index))
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

func GetTeacherHandler(c *gin.Context) {
	index, err := strconv.Atoi(c.Param("index"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid index",
		})
		return
	}

	teacher, err := datastore.GetTeacher(uint32(index))
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

func GetRoomHandler(c *gin.Context) {
	index, err := strconv.Atoi(c.Param("index"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid index",
		})
		return
	}

	room, err := datastore.GetRoom(uint32(index))
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