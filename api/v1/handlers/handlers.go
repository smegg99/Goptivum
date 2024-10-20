// handlers/handlers.go
package handlers

import (
	"fmt"
	"net/http"
	"strings"

	"smuggr.xyz/optivum-bsf/common/config"
	"smuggr.xyz/optivum-bsf/common/models"

	"github.com/gin-gonic/gin"
	"google.golang.org/protobuf/proto"
)

var Config *config.APIConfig

func Respond(c *gin.Context, code int, data interface{}) {
    accept := c.GetHeader("Accept")
    switch {
    case strings.Contains(accept, "application/protobuf"):
        protoMsg, ok := data.(proto.Message)
        if !ok {
            c.ProtoBuf(http.StatusInternalServerError, models.APIResponse{
                Message: "internal server error",
                Success: false,
            })
            return
        }
        c.ProtoBuf(code, protoMsg)
    case strings.Contains(accept, "application/json"):
        fallthrough
    default:
        c.JSON(code, data)
    }
}

func PingHandler(c *gin.Context) {
	Respond(c, http.StatusOK, models.APIResponse{
        Message: "pong",
        Success: true,
    })
}

func Initialize() {
	fmt.Println("initializing handlers")
	Config = &config.Global.API
}