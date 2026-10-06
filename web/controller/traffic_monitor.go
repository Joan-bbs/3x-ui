package controller

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/mhsanaei/3x-ui/v3/web/service"
)

type TrafficMonitorController struct {
	monitor *service.InboundTrafficMonitor
}

func NewTrafficMonitorController(m *service.InboundTrafficMonitor) *TrafficMonitorController {
	return &TrafficMonitorController{monitor: m}
}

func (c *TrafficMonitorController) GetCurrentTraffic(ctx *gin.Context) {
	if c.monitor == nil {
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"msg": "monitor not initialized"})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{
		"msg":  "success",
		"data": c.monitor.GetCurrentTraffic(),
	})
}

func (c *TrafficMonitorController) GetHistory(ctx *gin.Context) {
	if c.monitor == nil {
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"msg": "monitor not initialized"})
		return
	}

	inboundID, err := strconv.Atoi(ctx.Query("inbound_id"))
	if err != nil || inboundID <= 0 {
		ctx.JSON(http.StatusBadRequest, gin.H{"msg": "invalid inbound_id"})
		return
	}

	hours := 24
	if h, err := strconv.Atoi(ctx.Query("hours")); err == nil && h > 0 {
		hours = h
	}

	rows, err := c.monitor.GetHistoryByInbound(inboundID, hours)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"msg": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"msg":  "success",
		"data": rows,
	})
}

func (c *TrafficMonitorController) GetConfig(ctx *gin.Context) {
	if c.monitor == nil {
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"msg": "monitor not initialized"})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"msg":  "success",
		"data": c.monitor.GetConfig(),
	})
}

func (c *TrafficMonitorController) SetRefreshInterval(ctx *gin.Context) {
	if c.monitor == nil {
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"msg": "monitor not initialized"})
		return
	}

	var req struct {
		Interval int `json:"interval"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil || req.Interval == 0 {
		ctx.JSON(http.StatusBadRequest, gin.H{"msg": "invalid interval"})
		return
	}

	if err := c.monitor.SetRefreshInterval(req.Interval); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"msg": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"msg": "success"})
}

func (c *TrafficMonitorController) SetRetentionDays(ctx *gin.Context) {
	if c.monitor == nil {
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"msg": "monitor not initialized"})
		return
	}

	var req struct {
		Days int `json:"days"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil || req.Days == 0 {
		ctx.JSON(http.StatusBadRequest, gin.H{"msg": "invalid days"})
		return
	}

	if err := c.monitor.SetRetentionDays(req.Days); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"msg": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"msg": "success"})
}
