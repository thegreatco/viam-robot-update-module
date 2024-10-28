package main

import (
	"go.viam.com/rdk/components/generic"
	"go.viam.com/rdk/module"
	"go.viam.com/utils"

	"viam-robot-update-module/update_module"
	module_utils "viam-robot-update-module/utils"

	viamutils "github.com/thegreatco/viamutils/module"
)

func main() {
	viamutils.AddModularResource(generic.API, update_module.Model)
	utils.ContextualMain(viamutils.RunModule, module.NewLoggerFromArgs(module_utils.LoggerName))
}
