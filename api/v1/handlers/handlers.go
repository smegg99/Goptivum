package handlers

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"smuggr.xyz/optivum-bsf/common/config"
)

var Config *config.APIConfig

func PingHandler(c *gin.Context) {
	c.JSON(200, gin.H{
		"message": "Pong",
	})
}

func Initialize() {
	fmt.Println("Initializing handlers")
	Config = &config.Global.API
}