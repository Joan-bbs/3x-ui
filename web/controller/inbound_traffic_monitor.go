package controller

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/mhsanaei/3x-ui/v3/logger"
	"github.com/mhsanaei/3x-ui/v3/web/entity"
	"github.com/mhsanaei/3x-ui/v3/web/service"
)

type TrafficMonitorController struct {
	trafficMonitor *service.InboundTrafficMonitor
}

// NewTrafficMonitorController creates a new traffic monitor controller
func NewTrafficMonitorController(tm *service.InboundTrafficMonitor) *TrafficMonitorController {
	return &TrafficMonitorController{
		trafficMonitor: tm,
	}
}

// GetCurrentTraffic returns current traffic snapshot for all inbounds
func (c *TrafficMonitorController) GetCurrentTraffic(ctx *gin.Context) {
	if c.trafficMonitor == nil {
		ctx.JSON(http.StatusInternalServerError, entity.NewError("Traffic monitor not initialized"))
		return
	}

	snapshots := c.trafficMonitor.GetCurrentTraffic()
	ctx.JSON(http.StatusOK, entity.NewMsgData(snapshots))
}

// GetTrafficHistory returns historical traffic data for a specific inbound
func (c *TrafficMonitorController) GetTrafficHistory(ctx *gin.Context) {
	if c.trafficMonitor == nil {
		ctx.JSON(http.StatusInternalServerError, entity.NewError("Traffic monitor not initialized"))
		return
	}

	inboundIDStr := ctx.Query("inbound_id")
	hoursStr := ctx.Query("hours")

	if inboundIDStr == "" {
		ctx.JSON(http.StatusBadRequest, entity.NewError("inbound_id is required"))
		return
	}

	inboundID, err := strconv.ParseUint(inboundIDStr, 10, 32)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, entity.NewError("Invalid inbound_id"))
		return
	}

	hours := 24
	if hoursStr != "" {
		if h, err := strconv.Atoi(hoursStr); err == nil && h > 0 {
			hours = h
		}
	}

	histories, err := c.trafficMonitor.GetTrafficHistory(uint(inboundID), hours)
	if err != nil {
		logger.Warningf("Failed to get traffic history: %v", err)
		ctx.JSON(http.StatusInternalServerError, entity.NewError("Failed to get traffic history"))
		return
	}

	ctx.JSON(http.StatusOK, entity.NewMsgData(histories))
}

// GetMonitoringConfig returns current monitoring configuration
func (c *TrafficMonitorController) GetMonitoringConfig(ctx *gin.Context) {
	if c.trafficMonitor == nil {
		ctx.JSON(http.StatusInternalServerError, entity.NewError("Traffic monitor not initialized"))
		return
	}

	config := c.trafficMonitor.GetConfig()
	ctx.JSON(http.StatusOK, entity.NewMsgData(config))
}

// SetRefreshInterval sets the monitoring refresh interval
func (c *TrafficMonitorController) SetRefreshInterval(ctx *gin.Context) {
	if c.trafficMonitor == nil {
		ctx.JSON(http.StatusInternalServerError, entity.NewError("Traffic monitor not initialized"))
		return
	}

	var req struct {
		Interval int `json:"interval" binding:"required"`
	}

	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, entity.NewError("Invalid request"))
		return
	}

	if err := c.trafficMonitor.SetRefreshInterval(req.Interval); err != nil {
		ctx.JSON(http.StatusBadRequest, entity.NewError(err.Error()))
		return
	}

	ctx.JSON(http.StatusOK, entity.NewMsg("Refresh interval updated"))
}

// SetRetentionDays sets the historical data retention period
func (c *TrafficMonitorController) SetRetentionDays(ctx *gin.Context) {
	if c.trafficMonitor == nil {
		ctx.JSON(http.StatusInternalServerError, entity.NewError("Traffic monitor not initialized"))
		return
	}

	var req struct {
		Days int `json:"days" binding:"required"`
	}

	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, entity.NewError("Invalid request"))
		return
	}

	if err := c.trafficMonitor.SetRetentionDays(req.Days); err != nil {
		ctx.JSON(http.StatusBadRequest, entity.NewError(err.Error()))
		return
	}

	ctx.JSON(http.StatusOK, entity.NewMsg("Retention days updated"))
}

// StartMonitoring starts the traffic monitoring
func (c *TrafficMonitorController) StartMonitoring(ctx *gin.Context) {
	if c.trafficMonitor == nil {
		ctx.JSON(http.StatusInternalServerError, entity.NewError("Traffic monitor not initialized"))
		return
	}

	if err := c.trafficMonitor.StartMonitoring(); err != nil {
		ctx.JSON(http.StatusBadRequest, entity.NewError(err.Error()))
		return
	}

	ctx.JSON(http.StatusOK, entity.NewMsg("Monitoring started"))
}

// StopMonitoring stops the traffic monitoring
func (c *TrafficMonitorController) StopMonitoring(ctx *gin.Context) {
	if c.trafficMonitor == nil {
		ctx.JSON(http.StatusInternalServerError, entity.NewError("Traffic monitor not initialized"))
		return
	}

	c.trafficMonitor.StopMonitoring()
	ctx.JSON(http.StatusOK, entity.NewMsg("Monitoring stopped"))
}
