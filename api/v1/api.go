package v1

import (
	"fmt"
	"os"
	"strconv"

	"smuggr.xyz/optivum-bsf/api/v1/routes"
	"smuggr.xyz/optivum-bsf/common/config"

	"github.com/gin-gonic/gin"
)

var DefaultRouter *gin.Engine
var Config *config.APIConfig

func Initialize() (chan error) {
	fmt.Println("initializing api/v1")

	Config = &config.Global.API
	gin.SetMode(os.Getenv("GIN_MODE"))

	DefaultRouter = gin.Default()
	//SetupCors()

	routes.Initialize(DefaultRouter)

	errCh := make(chan error)
	go func() {
		err := DefaultRouter.Run(":" + strconv.Itoa(int(Config.Port)))
		errCh <- err
	}()

	return errCh
}
