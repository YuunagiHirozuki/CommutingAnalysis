// Package api 提供 REST API(Gin)。
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"backend/internal/config"
	"backend/internal/service"
)

// Server API 服务器,持有全部已加载城市的引擎。
type Server struct {
	Cfg     *config.Config
	Engines map[string]*service.Engine
	client  *http.Client // 外部服务(地址搜索)用;支持配置代理
}

// NewRouter 构建路由(含开发环境 CORS)。
func NewRouter(cfg *config.Config, engines map[string]*service.Engine) *gin.Engine {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if cfg.Network.ProxyURL != "" {
		if pu, err := url.Parse(cfg.Network.ProxyURL); err == nil {
			transport.Proxy = http.ProxyURL(pu)
		}
	}
	s := &Server{
		Cfg:     cfg,
		Engines: engines,
		client:  &http.Client{Timeout: 8 * time.Second, Transport: transport},
	}
	r := gin.Default()
	r.Use(cors())

	r.GET("/api/health", s.health)
	r.GET("/api/cities", s.cities)
	r.GET("/api/geocode", s.geocode)
	r.GET("/api/stations/nearby", s.nearby)
	r.POST("/api/isochrone", s.isochrone)
	return r
}

func cors() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

// engine 按请求参数解析城市;city 缺省取配置中的第一个。
func (s *Server) engine(c *gin.Context) (*service.Engine, bool) {
	code := c.Query("city")
	if code == "" {
		code = s.Cfg.Cities[0].Code
	}
	eng, ok := s.Engines[code]
	if !ok {
		badRequest(c, fmt.Sprintf("未知城市 %q,可用: %v", code, s.cityCodes()))
		return nil, false
	}
	return eng, true
}

func (s *Server) cityCodes() []string {
	codes := make([]string, 0, len(s.Engines))
	for _, ct := range s.Cfg.Cities {
		if _, ok := s.Engines[ct.Code]; ok {
			codes = append(codes, ct.Code)
		}
	}
	return codes
}

func (s *Server) health(c *gin.Context) {
	cities := make([]gin.H, 0, len(s.Engines))
	for _, ct := range s.Cfg.Cities {
		eng, ok := s.Engines[ct.Code]
		if !ok {
			continue
		}
		cities = append(cities, gin.H{
			"code": ct.Code, "name": eng.City.Name, "nodes": eng.Graph.NodeCount(),
			"arcs": eng.Graph.ArcCount(), "loadMs": eng.LoadMs,
			"dataDir": eng.City.DataDir, "testData": eng.IsTest,
		})
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "cities": cities})
}

func (s *Server) cities(c *gin.Context) {
	out := make([]gin.H, 0, len(s.Engines))
	for _, ct := range s.Cfg.Cities {
		eng, ok := s.Engines[ct.Code]
		if !ok {
			continue
		}
		out = append(out, gin.H{
			"code": ct.Code, "name": eng.City.Name, "bbox": ct.BBox, "testData": eng.IsTest,
		})
	}
	c.JSON(http.StatusOK, gin.H{"cities": out})
}

// geocode 地址搜索:代理 Nominatim(OSM),后端转发以设置合规 User-Agent 并规避浏览器跨域。
func (s *Server) geocode(c *gin.Context) {
	q := c.Query("q")
	if q == "" {
		badRequest(c, "缺少 q 参数")
		return
	}
	eng, ok := s.engine(c)
	if !ok {
		return
	}
	b := eng.City.BBox // [minLon, minLat, maxLon, maxLat]
	vb := fmt.Sprintf("%v,%v,%v,%v", b[0], b[3], b[2], b[1]) // Nominatim: left,top,right,bottom
	u := fmt.Sprintf(
		"https://nominatim.openstreetmap.org/search?format=jsonv2&limit=6&accept-language=zh-CN&bounded=1&viewbox=%s&q=%s",
		vb, url.QueryEscape(q))
	req, _ := http.NewRequest(http.MethodGet, u, nil)
	req.Header.Set("User-Agent", "CommuteAnalysis-MVP/0.1 (university graduation project)")
	resp, err := s.client.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "地址服务请求失败: " + err.Error()})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("地址服务返回 %d", resp.StatusCode)})
		return
	}
	var raw []struct {
		DisplayName string `json:"display_name"`
		Lat         string `json:"lat"`
		Lon         string `json:"lon"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "地址服务响应解析失败"})
		return
	}
	out := make([]gin.H, 0, len(raw))
	for _, r := range raw {
		lat, _ := strconv.ParseFloat(r.Lat, 64)
		lon, _ := strconv.ParseFloat(r.Lon, 64)
		if lat == 0 && lon == 0 {
			continue
		}
		out = append(out, gin.H{"name": r.DisplayName, "latitude": lat, "longitude": lon})
	}
	c.JSON(http.StatusOK, gin.H{"results": out})
}

func (s *Server) nearby(c *gin.Context) {
	eng, ok := s.engine(c)
	if !ok {
		return
	}
	lat, err := strconv.ParseFloat(c.Query("latitude"), 64)
	if err != nil {
		badRequest(c, "latitude 参数非法")
		return
	}
	lon, err := strconv.ParseFloat(c.Query("longitude"), 64)
	if err != nil {
		badRequest(c, "longitude 参数非法")
		return
	}
	radius := 1000.0
	if v := c.Query("radius"); v != "" {
		if radius, err = strconv.ParseFloat(v, 64); err != nil || radius <= 0 || radius > 10000 {
			badRequest(c, "radius 参数非法(0, 10000]")
			return
		}
	}
	limit := 20
	if v := c.Query("limit"); v != "" {
		if limit, err = strconv.Atoi(v); err != nil || limit <= 0 || limit > 100 {
			badRequest(c, "limit 参数非法(0, 100]")
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"stations": eng.NearbyStations(lat, lon, radius, limit)})
}

type isochroneReq struct {
	Latitude  float64 `json:"latitude" binding:"required"`
	Longitude float64 `json:"longitude" binding:"required"`
	MaxTime   float64 `json:"maxTime" binding:"required"`
	City      string  `json:"city"`
}

func (s *Server) isochrone(c *gin.Context) {
	var req isochroneReq
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, "请求体非法:{latitude, longitude, maxTime} 均为必填数字")
		return
	}
	if req.MaxTime != 30 && req.MaxTime != 45 && req.MaxTime != 60 && req.MaxTime != 90 {
		badRequest(c, "maxTime 仅支持 30 / 45 / 60 / 90(分钟)")
		return
	}
	code := req.City
	if code == "" {
		code = s.Cfg.Cities[0].Code
	}
	eng, ok := s.Engines[code]
	if !ok {
		badRequest(c, fmt.Sprintf("未知城市 %q,可用: %v", code, s.cityCodes()))
		return
	}
	if !checkCoord(c, req.Latitude, req.Longitude) {
		return
	}
	result, err := eng.Isochrone(service.IsochroneRequest{
		Latitude: req.Latitude, Longitude: req.Longitude, MaxTime: req.MaxTime,
	})
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"city":              eng.City.Code,
		"center":            gin.H{"latitude": req.Latitude, "longitude": req.Longitude},
		"maxTime":           result.MaxTime,
		"reachableStations": result.ReachableStations,
		"entryStations":     result.EntryStations,
		"computeMs":         result.ComputeMs,
		"gridCells":         result.GridCells,
		"testData":          result.TestData,
		"notice":            result.Notice,
		"geojson":           result.GeoJSON,
	})
}

// checkCoord 仅做经纬度合法性与范围检查;城市数据覆盖范围外的位置
// 不再拒绝,由 service 层按最近站点计算并在结果中返回 notice 提示。
func checkCoord(c *gin.Context, lat, lon float64) bool {
	if lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		badRequest(c, "坐标超出经纬度范围")
		return false
	}
	return true
}

func badRequest(c *gin.Context, msg string) {
	c.JSON(http.StatusBadRequest, gin.H{"error": msg})
}
