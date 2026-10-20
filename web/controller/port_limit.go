package controller

import (
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/mhsanaei/3x-ui/v2/web/portlimit"
	"github.com/mhsanaei/3x-ui/v2/web/service"
	"github.com/mhsanaei/3x-ui/v2/web/session"
	"net"
	"os/exec"
)

func (a *InboundController) getPortLimits(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	user := session.GetLoginUser(c)
	inbounds, err := a.inboundService.GetInboundsForMonitor(user.Id)
	if err != nil {
		jsonObj(c, nil, err)
		return
	}
	saved, status := service.PortLimits.Snapshot(user.Id)
	interfaces := []string{}
	nics, _ := net.Interfaces()
	for _, nic := range nics {
		if nic.Flags&net.FlagLoopback == 0 && nic.Flags&net.FlagUp != 0 {
			interfaces = append(interfaces, nic.Name)
		}
	}
	suggested := ""
	if out, err := exec.Command("ip", "-j", "route", "show", "default").Output(); err == nil {
		var routes []struct {
			Dev string `json:"dev"`
		}
		if json.Unmarshal(out, &routes) == nil && len(routes) > 0 {
			suggested = routes[0].Dev
		}
	}
	rows := []gin.H{}
	seen := map[int]bool{}
	for _, in := range inbounds {
		row := gin.H{"id": in.Id, "port": in.Port, "remark": in.Remark, "protocol": in.Protocol, "enable": in.Enable, "upload": 0, "download": 0, "interface": suggested, "configured": false, "orphan": false}
		for _, r := range saved {
			if r.ID == in.Id && r.Port == in.Port {
				row["upload"] = r.Upload
				row["download"] = r.Download
				row["interface"] = r.Interface
				row["configured"] = true
				seen[r.Port] = true
			}
		}
		rows = append(rows, row)
	}
	for _, r := range saved {
		if !seen[r.Port] {
			rows = append(rows, gin.H{"id": r.ID, "port": r.Port, "remark": "原入站已删除或改端口，请取消旧限速", "interface": r.Interface, "upload": r.Upload, "download": r.Download, "configured": true, "orphan": true})
		}
	}
	_, tcErr := exec.LookPath("tc")
	jsonObj(c, gin.H{"rows": rows, "interfaces": interfaces, "error": status, "available": tcErr == nil}, nil)
}

func (a *InboundController) setPortLimit(c *gin.Context) {
	var req struct {
		ID        int    `form:"id"`
		Port      int    `form:"port"`
		Interface string `form:"interface"`
		Upload    int    `form:"upload"`
		Download  int    `form:"download"`
	}
	if err := c.ShouldBind(&req); err != nil {
		jsonObj(c, nil, errors.New("无效参数"))
		return
	}
	user := session.GetLoginUser(c)
	r := portlimit.Rule{ID: req.ID, UserID: user.Id, Port: req.Port, Interface: req.Interface, Upload: req.Upload, Download: req.Download}
	removing := req.Upload == 0 && req.Download == 0
	saved, _ := service.PortLimits.Snapshot(user.Id)
	ownedSaved := false
	for _, old := range saved {
		if old.ID == req.ID && old.Port == req.Port {
			ownedSaved = true
			if removing {
				r.Interface = old.Interface
			}
		}
	}
	inbounds, err := a.inboundService.GetInboundsForMonitor(user.Id)
	if err != nil {
		jsonObj(c, nil, err)
		return
	}
	owned := false
	for _, in := range inbounds {
		if in.Id == req.ID && in.Port == req.Port {
			owned = true
		}
	}
	if !owned && !(removing && ownedSaved) {
		jsonObj(c, nil, errors.New("只能设置当前账号入站的端口"))
		return
	}
	if !removing {
		all, err := a.inboundService.GetAllInbounds()
		if err != nil {
			jsonObj(c, nil, err)
			return
		}
		for _, in := range all {
			if in.Port == req.Port && in.UserId != user.Id {
				jsonObj(c, nil, errors.New("该端口同时属于其他账号，无法按端口限速"))
				return
			}
		}
		var settings service.SettingService
		panelPort, err := settings.GetPort()
		if err != nil {
			jsonObj(c, nil, err)
			return
		}
		if panelPort == req.Port {
			jsonObj(c, nil, errors.New("不能限制面板自身端口"))
			return
		}
	}
	err = service.PortLimits.Set(r)
	jsonObj(c, nil, err)
}
