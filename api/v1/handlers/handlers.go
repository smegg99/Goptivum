package handlers

import (
	"fmt"
	"net/http"
	"strings"

	"smuggr.xyz/optivum-bsf/common/config"

	"github.com/gin-gonic/gin"
	"google.golang.org/protobuf/proto"
)

var Config *config.APIConfig

func Respond(c *gin.Context, data interface{}) {
    accept := c.GetHeader("Accept")
    switch {
    case strings.Contains(accept, "application/protobuf"):
        protoMsg, ok := data.(proto.Message)
        if !ok {
            c.JSON(http.StatusInternalServerError, gin.H{"error": "Invalid protobuf message"})
            return
        }
        c.ProtoBuf(http.StatusOK, protoMsg)
    case strings.Contains(accept, "application/json"):
        fallthrough
    default:
        c.JSON(http.StatusOK, data)
    }
}

func PingHandler(c *gin.Context) {
	c.JSON(200, gin.H{
		"message": "Pong",
	})
}

func Initialize() {
	fmt.Println("initializing handlers")
	Config = &config.Global.API
}