// api/v1/errors/response.go
package errors

import "github.com/gin-gonic/gin"

func RespondWithError(c *gin.Context, err HTTPError) {
	c.JSON(err.StatusCode, gin.H{
		"error": gin.H{
			"code":    err.Code,
			"message": err.Message,
		},
	})
}

func RespondWithCustomError(c *gin.Context, err HTTPError, customMessage string) {
	c.JSON(err.StatusCode, gin.H{
		"error": gin.H{
			"code":    err.Code,
			"message": customMessage,
		},
	})
}

func AbortWithError(c *gin.Context, err HTTPError) {
	c.AbortWithStatusJSON(err.StatusCode, gin.H{
		"error": gin.H{
			"code":    err.Code,
			"message": err.Message,
		},
	})
}
