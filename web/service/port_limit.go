package service

import (
	"github.com/mhsanaei/3x-ui/v2/config"
	"github.com/mhsanaei/3x-ui/v2/web/portlimit"
	"path/filepath"
)

var PortLimits = portlimit.New(filepath.Join(config.GetDBFolderPath(), "port-limits.json"), nil)
